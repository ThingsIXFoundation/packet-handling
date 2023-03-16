// Copyright 2022 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package forwarder

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/ThingsIXFoundation/packet-handling/airtime"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/events"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/lorawan"
	"github.com/chirpstack/chirpstack/api/go/v4/gw"
	"github.com/sirupsen/logrus"
)

// Exchange has several tasks:
// - it provides a backend on which trusted gateways can connect
// - it connects to ThingsIX routers
// - it keeps a routing table to exchange data between gateways and routers
type Exchange struct {
	// chirpstack backend that handles interaction with connected gateways
	backend Backend
	// accounter is used to determine if a packet must be routed between a
	// gateway and router because the router owner has paid for it.
	accounter Accounter
	// store with gateways that are allowed to use this exchange
	gateways gateway.GatewayStore
	// recordUnknownGateway is called each time a gateway connects that is not
	// in the gateway store
	recordUnknownGateway gateway.UnknownGatewayLogger
	// routes holds the required information to exchange data with
	// external ThingsIX routers
	routingTable *RoutingTable

	mapperForwarder *MapperForwarder
}

// NewExchange instantiates a new packet exchange where gateways and
// routers can exchange packets.
func NewExchange(ctx context.Context, cfg *Config) (*Exchange, error) {
	// currently allow all gateways. Once the forwarders exchange only forwards
	// packets for onboarded gateway we need to use a filtered gateway store
	// that filters out gateways that are not onboarded.
	store, err := gateway.NewGatewayStore(ctx,
		&cfg.Forwarder.Gateways.Store, &cfg.Forwarder.Gateways.Registry)
	if err != nil {
		logrus.WithError(err).Fatal("unable to load gateway store")
	}

	// create gateway backend
	backend, err := buildBackend(cfg)
	if err != nil {
		return nil, err
	}

	// build data accounter
	accounter, err := buildAccounter(cfg)
	if err != nil {
		return nil, err
	}

	// build routing table to determine where data must be forwarded to
	routingTable, err := buildRoutingTable(cfg, store, accounter)
	if err != nil {
		return nil, err
	}

	// create a logger that logs gateways that have not been seen earlier
	recorder := gateway.NewUnknownGatewayLogger(cfg.Forwarder.Gateways.RecordUnknown)

	// instantiate exchange
	exchange := &Exchange{
		backend:              backend,
		accounter:            accounter,
		routingTable:         routingTable,
		gateways:             store,
		recordUnknownGateway: recorder,
	}

	if exchange.mapperForwarder, err = NewMapperForwarder(exchange, store); err != nil {
		return nil, err
	}

	// backend uses callbacks to inform the exchange of events
	backend.SetUplinkFrameFunc(exchange.uplinkFrameCallback)
	backend.SetDownlinkTxAckFunc(exchange.downlinkTxAck)
	backend.SetGatewayStatsFunc(exchange.gatewayStats)
	backend.SetSubscribeEventFunc(exchange.subscribeEvent)
	backend.SetRawPacketForwarderEventFunc(nil) // TODO:??

	return exchange, nil
}

// Run the exchange until the given ctx expires.
func (e *Exchange) Run(ctx context.Context) {
	// run the gateway store background tasks
	go e.gateways.Run(ctx)

	// start backend and accept gateways
	err := e.backend.Start()
	if err != nil {
		logrus.WithError(err).Fatal("could not start backend")
	}

	// update the routing table periodically
	go e.routingTable.Run(ctx)

	// wait for messages from the network and dispatch them to the chirpstack backend
	for {
		select {
		case in, ok := <-e.routingTable.networkEvents: // incoming event from the network
			if ok {
				if frame := in.event.GetDownlinkFrameEvent(); frame != nil {
					e.handleDownlinkFrame(frame)
				} else if airtimePayment := in.event.GetAirtimePaymentEvent(); airtimePayment != nil {
					e.accounter.AddPayment(airtimePayment)
				} else {
					logrus.WithFields(logrus.Fields{
						"source": in.source.Endpoint,
						"event":  fmt.Sprintf("%T", in.event),
					}).Error("received unsupported network event")
				}
			}
		case <-ctx.Done():
			err := e.backend.Stop()
			if err != nil {
				logrus.WithError(err).Error("could not stop backend, stopping anyway")
			}
			logrus.Info("packet exchange stopped")
			return
		}
	}
}

func (e *Exchange) uplinkFrameCallback(frame *gw.UplinkFrame) {
	gatewayLocalID, err := utils.Eui64FromString(frame.GetRxInfo().GetGatewayId())
	if err != nil {
		logrus.WithError(err).Warn("received uplink from gateway with invalid gateway ID")
		return
	}

	log := logrus.WithField("gw_local_id", gatewayLocalID)

	// ensure that received frame is from a trusted gateway if not drop it
	gw, err := e.gateways.ByLocalIDString(frame.RxInfo.GatewayId)
	if err != nil {
		uplinksCounter.WithLabelValues(lorawan.EUI64{}.String(), "failed").Inc()
		log.Warn("uplink from unknown gateway, drop packet")
		_ = e.recordUnknownGateway.Record(gatewayLocalID)
		return
	}

	// log frame details
	log = log.WithField("gw_network_id", gw.NetworkID)
	frameLog := log.WithFields(logrus.Fields{
		"rssi":        frame.GetRxInfo().GetRssi(),
		"snr":         frame.GetRxInfo().GetSnr(),
		"freq":        frame.GetTxInfo().GetFrequency(),
		"sf":          frame.GetTxInfo().GetModulation().GetLora().GetSpreadingFactor(),
		"pol":         frame.GetTxInfo().GetModulation().GetLora().GetPolarizationInversion(),
		"coderate":    frame.GetTxInfo().GetModulation().GetLora().GetCodeRate(),
		"bandwidth":   frame.GetTxInfo().GetModulation().GetLora().GetBandwidth(),
		"payload":     base64.RawStdEncoding.EncodeToString(frame.GetPhyPayload()),
		"payload_len": len(frame.GetPhyPayload()),
	})

	// convert the frame from its local format (gateway <-> exchange) into its network
	// representation (exchange <-> router) so it can be broadcasted onto the network
	if frame, err = localUplinkFrameToNetwork(gw, frame); err != nil {
		uplinksCounter.WithLabelValues(gw.NetworkID.String(), "failed").Inc()
		frameLog.WithError(err).Error("update uplink frame to network format failed, drop packet")
		return
	}

	// decode it into a lorawan packet to determine what needs to be done
	var phy lorawan.PHYPayload
	if err := phy.UnmarshalBinary(frame.PhyPayload); err != nil {
		uplinksCounter.WithLabelValues(gw.NetworkID.String(), "failed").Inc()
		frameLog.WithError(err).Error("could not decode lorawan packet, drop packet")
		return
	}

	airtime, _ := airtime.UplinkAirtime(frame)

	// Add some metadata to the frame that will be forwarded to end-applications
	setChaindataInFrameMetadata(frame, gw, airtime)

	frameLog = frameLog.WithFields(logrus.Fields{
		"type":    phy.MHDR.MType,
		"airtime": airtime,
	})

	switch phy.MHDR.MType {
	case lorawan.ConfirmedDataUp, lorawan.UnconfirmedDataUp:
		// Filter by NetID
		mac, ok := phy.MACPayload.(*lorawan.MACPayload)
		if !ok {
			uplinksCounter.WithLabelValues(gw.NetworkID.String(), "failed").Inc()
			frameLog.Error("invalid packet: data-up but no mac-payload, drop packet")
			return
		}
		frameLog = frameLog.WithFields(logrus.Fields{
			"dev_addr": mac.FHDR.DevAddr,
			"fcnt":     mac.FHDR.FCnt,
		})

		// check if the packet received could be a mapper packet and process it
		if IsMaybeMapperPacket(frame, mac) {
			e.mapperForwarder.HandleMapperPacket(frame, mac)
			return
		}

		event := router.GatewayToRouterEvent{
			GatewayInformation: &router.GatewayInformation{
				PublicKey: gw.CompressedPubKeyBytes(),
				Owner:     gw.OwnerBytes(),
			},
			Event: &router.GatewayToRouterEvent_UplinkFrameEvent{
				UplinkFrameEvent: &router.UplinkFrameEvent{
					UplinkFrame: frame,
					AirtimeReceipt: &router.AirtimeReceipt{
						Owner:   gw.OwnerBytes(),
						Airtime: uint32(airtime.Milliseconds()),
					},
				},
			},
		}

		// packet is valid, router clients are subscribed to this uplink broadcaster
		// and will receive it. If the router they are connected to is interested in
		// the package it will send the packet to the router.
		if !e.routingTable.gatewayEvents.TryBroadcast(&GatewayEvent{
			uplink: &struct {
				device lorawan.DevAddr
				event  *router.GatewayToRouterEvent
			}{
				device: mac.FHDR.DevAddr,
				event:  &event,
			},
			receivedFrom: gw,
		}) {
			uplinksCounter.WithLabelValues(gw.NetworkID.String(), "failed").Inc()
			frameLog.Warn("unable to broadcast uplink to routing table, drop packet")
		} else {
			uplinksCounter.WithLabelValues(gw.NetworkID.String(), "success").Inc()
			frameLog.Info("received packet")
		}
	case lorawan.JoinRequest, lorawan.RejoinRequest:
		// Filter by Xor8 filter on devEUI
		jr, ok := phy.MACPayload.(*lorawan.JoinRequestPayload)
		if !ok {
			log.Error("invalid packet: join but no join-payload, drop packet")
			return
		}

		frameLog = frameLog.WithFields(logrus.Fields{
			"dev_eui": jr.DevEUI,
		})

		// Join is internally an Uplink
		event := router.GatewayToRouterEvent{
			GatewayInformation: &router.GatewayInformation{
				PublicKey: gw.CompressedPubKeyBytes(),
				Owner:     gw.OwnerBytes(),
			},
			Event: &router.GatewayToRouterEvent_UplinkFrameEvent{
				UplinkFrameEvent: &router.UplinkFrameEvent{
					UplinkFrame: frame,
					AirtimeReceipt: &router.AirtimeReceipt{
						Owner:   gw.OwnerBytes(),
						Airtime: uint32(airtime.Milliseconds()),
					},
				},
			},
		}

		// packet is valid, router clients are subscribed to this uplink broadcaster
		// and will receive it. If the router they are connected to is interested in
		// the package it will send the packet to the router.
		if !e.routingTable.gatewayEvents.TryBroadcast(&GatewayEvent{
			receivedFrom: gw,
			join: &struct {
				devEUI lorawan.EUI64
				event  *router.GatewayToRouterEvent
			}{
				jr.DevEUI, &event,
			},
		}) {
			uplinksCounter.WithLabelValues(gw.NetworkID.String(), "failed").Inc()
			frameLog.Warn("unable to broadcast uplink to routing table, drop packet")
		} else {
			uplinksCounter.WithLabelValues(gw.NetworkID.String(), "success").Inc()
			frameLog.Info("received packet")
		}
	}
}

func (e *Exchange) gatewayStats(stats *gw.GatewayStats) {
	gw, err := e.gateways.ByLocalIDString(stats.GetGatewayId())
	if err != nil {
		logrus.Warn("gateway stats from unknown gateway, drop stats")
		return
	}

	var (
		log = logrus.WithFields(logrus.Fields{
			"gw_network_id": gw.NetworkID,
			"gw_local_id":   gw.LocalID,
		})
		gatewayNetworkID = gw.NetworkID.String()
	)

	log.Debug("received gateway stats")

	for f, n := range stats.RxPacketsPerFrequency {
		gatewayRxPacketsPerFrequencyGauge.WithLabelValues(gatewayNetworkID, fmt.Sprintf("%d", f)).Set(float64(n))
	}
	for _, m := range stats.RxPacketsPerModulation {
		lora := m.GetModulation().GetLora()
		gatewayRxPacketsPerModulationGauge.WithLabelValues(gatewayNetworkID,
			fmt.Sprintf("%d", lora.GetBandwidth()),
			fmt.Sprintf("%d", lora.GetSpreadingFactor()),
			lora.GetCodeRate().String()).Set(float64(m.GetCount()))
	}
	gatewayRxReceivedGauge.WithLabelValues(gatewayNetworkID, "success").Set(float64(stats.RxPacketsReceived))
	gatewayTxEmittedGauge.WithLabelValues(gatewayNetworkID).Set(float64(stats.TxPacketsEmitted))
	for f, n := range stats.TxPacketsPerFrequency {
		gatewayTxPacketsPerFrequencyGauge.WithLabelValues(gatewayNetworkID, fmt.Sprintf("%d", f)).Set(float64(n))
	}
	for _, m := range stats.TxPacketsPerModulation {
		lora := m.GetModulation().GetLora()
		gatewayTxPacketsPerModulationGauge.WithLabelValues(gatewayNetworkID,
			fmt.Sprintf("%d", lora.GetBandwidth()),
			fmt.Sprintf("%d", lora.GetSpreadingFactor()),
			lora.GetCodeRate().String()).Set(float64(m.GetCount()))
	}

	for status, count := range stats.TxPacketsPerStatus {
		gatewayTxPacketsPerStatusGauge.WithLabelValues(gatewayNetworkID, status).Set(float64(count))
	}

	gatewayTxPacketsReceivedGauge.WithLabelValues(gatewayNetworkID).Set(float64(stats.TxPacketsReceived))
}

// subscribeEvent is called by the chirpstack backend, currently only when a gateway
// is online this callback is called.
func (e *Exchange) subscribeEvent(event events.Subscribe) {
	log := logrus.WithField("gw_local_id", hex.EncodeToString(event.GatewayID[:]))
	localGatewayID, err := gateway.BytesToGatewayID(event.GatewayID[:])
	if err != nil {
		log.Warn("event from gateway with invalid local id, drop event")
		return
	}

	// ensure that received frame is from a trusted gateway if not drop it
	gw, err := e.gateways.ByLocalID(localGatewayID)
	if err != nil {
		log.Warn("event from unknown gateway, drop event")
		_ = e.recordUnknownGateway.Record(localGatewayID)
		return
	}

	log = log.WithField("gw_network_id", gw.NetworkID)

	// event is valid, router clients are subscribed to this uplink broadcaster
	// and will receive it. If the router the client is connected to is
	// interested in the package it will send the packet to the router.
	if !e.routingTable.gatewayEvents.TryBroadcast(&GatewayEvent{
		receivedFrom: gw,
		subOnlineOfflineEvent: &struct {
			event *router.GatewayToRouterEvent
		}{
			&router.GatewayToRouterEvent{
				GatewayInformation: &router.GatewayInformation{
					PublicKey: gw.CompressedPubKeyBytes(),
					Owner:     gw.OwnerBytes(),
				},
				Event: &router.GatewayToRouterEvent_StatusEvent{
					StatusEvent: &router.StatusEvent{
						Online: event.Subscribe,
					},
				},
			},
		},
	}) {
		log.Warn("unable to broadcast gateway event to routers, drop event")
	}
}

func (e *Exchange) handleDownlinkFrame(event *router.DownlinkFrameEvent) {
	frame := event.GetDownlinkFrame()
	gwNetworkId, err := utils.Eui64FromString(frame.GetGatewayId())
	if err != nil {
		logrus.WithError(err).Errorf("unable to decode gateway-id: %s", frame.GetGatewayId())
	}

	log := logrus.WithField("gw_network_id", gwNetworkId)
	sink, err := e.gateways.ByNetworkID(gwNetworkId)

	if err != nil {
		downlinksCounter.WithLabelValues(gwNetworkId.String(), "failed").Inc()
		log.WithFields(logrus.Fields{
			"payload": base64.RawStdEncoding.EncodeToString(frame.Items[0].GetPhyPayload()),
		}).Warn("drop downlink frame - target gateway not found")
		return
	}

	log = log.WithField("gw_local_id", sink.LocalID)

	// convert the network downlink frame into a local frame
	frame = networkDownlinkFrameToLocal(sink, frame)

	// order backend to send the downlink to the gateway so it can be broadcasted
	if err := e.backend.SendDownlinkFrame(frame); err != nil {
		downlinksCounter.WithLabelValues(gwNetworkId.String(), "failed").Inc()
		log.WithError(err).Error("drop downlink: unable to send to gateway")
		return
	} else {
		downlinksCounter.WithLabelValues(gwNetworkId.String(), "ok").Inc()
		log.Info("downlink sent to backend")
	}
}

func (e *Exchange) downlinkTxAck(txack *gw.DownlinkTxAck) {
	var (
		log = logrus.WithFields(logrus.Fields{
			"gw_local_id": txack.GatewayId,
		})
	)
	log.Info("received downlink tx ack from gateway")

	localGatewayID, err := utils.Eui64FromString(txack.GetGatewayId())
	if err != nil {
		log.Errorf("received downlink ACK with invalid local gateway id: %s", txack.GetGatewayId())
		return
	}

	// ensure that received frame is from a trusted gateway if not drop it
	gw, err := e.gateways.ByLocalID(localGatewayID)
	if err != nil {
		downlinkTxAckCounter.WithLabelValues(fmt.Sprintf("%x", txack.GetGatewayId()), "failed").Inc()
		log.Warn("downlink tx ack from unknown gateway, drop packet")
		_ = e.recordUnknownGateway.Record(localGatewayID)
		return
	}
	log = log.WithField("gw_network_id", gw.NetworkID)

	// convert txack to network format
	if txack, err = localDownlinkTxAckToNetwork(gw, txack); err != nil {
		downlinkTxAckCounter.WithLabelValues(fmt.Sprintf("%x", txack.GetGatewayId()), "failed").Inc()
		logrus.WithError(err).Errorf("could update txack to network format")
		return
	}

	event := router.GatewayToRouterEvent{
		GatewayInformation: &router.GatewayInformation{
			PublicKey: gw.CompressedPubKeyBytes(),
			Owner:     gw.OwnerBytes(),
		},
		Event: &router.GatewayToRouterEvent_DownlinkTXAckEvent{
			DownlinkTXAckEvent: &router.DownlinkTXAckEvent{
				DownlinkTXAck: txack,
				AirtimeReceipt: &router.AirtimeReceipt{
					Owner: gw.OwnerBytes(),
					//TODO: Airtime: uint32(airtime.Milliseconds()),
				},
			},
		},
	}

	// send downlink ACK to the routing table that will forward it to the router
	if !e.routingTable.gatewayEvents.TryBroadcast(&GatewayEvent{
		receivedFrom: gw,
		downlinkAck: &struct {
			downlinkID uint32
			event      *router.GatewayToRouterEvent
		}{
			downlinkID: txack.GetDownlinkId(),
			event:      &event,
		},
	}) {
		downlinkTxAckCounter.WithLabelValues(fmt.Sprintf("%x", txack.GetGatewayId()), "failed").Inc()
		log.Warn("unable to broadcast downlink ACK to routing table, drop packet")
	} else {
		downlinkTxAckCounter.WithLabelValues(fmt.Sprintf("%x", txack.GetGatewayId()), "success").Inc()
	}
}

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
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/gofrs/uuid"
	"github.com/sirupsen/logrus"
	"github.com/zyedidia/generic/mapset"
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
	// set with gateways that are allowed to use this exchange
	trustedGateways *GatewaySet
	// recordUnknownGateway is called each time a gateway connects that is not
	// in the gateway store
	recordUnknownGateway UnknownGatewayLoggerFunc
	// routes holds the required information to exchange data with
	// external ThingsIX routers
	routingTable *RoutingTable

	mapperForwarder *MapperForwarder
}

// NewExchange instantiates a new packet exchange where gateways and
// routers can exchange packets.
func NewExchange(cfg *Config) (*Exchange, error) {
	store, err := loadGatewayStore(cfg)
	if err != nil {
		logrus.WithError(err).Fatal("unable to load gateway store")
	}

	// filter out gateways that are not yet onboarded and/or have their details set.
	trustedGatewaysByLocalID, trustedGatewaysByNetworkID, err := onboardedAndRegisteredGateways(cfg, store)
	if err != nil {
		return nil, err
	}

	logrus.WithField("#-gateways", len(trustedGatewaysByLocalID)).Info("loaded gateway store")

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
	routingTable, err := buildRoutingTable(cfg, accounter)
	if err != nil {
		return nil, err
	}

	// instantiate exchange
	exchange := &Exchange{
		backend:              backend,
		accounter:            accounter,
		routingTable:         routingTable,
		trustedGateways:      NewGatewaySet(cfg, store, trustedGatewaysByLocalID, trustedGatewaysByNetworkID),
		recordUnknownGateway: NewUnknownGatewayLogger(cfg),
	}

	if exchange.mapperForwarder, err = NewMapperForwarder(exchange); err != nil {
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
	// start backend and accept gateways
	err := e.backend.Start()
	if err != nil {
		logrus.WithError(err).Fatal("could not start backend")
	}

	// update the routing table periodically
	go e.routingTable.Run(ctx)

	// the exchange only operates on gateways that are onboarded on ThingsIX. Refresh
	// polls checks periodically if there are gateways onboarded/offboarded and refreshes
	// the trusted gateway set.
	go e.trustedGateways.Refresh(ctx)

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

func (e *Exchange) uplinkFrameCallback(frame gw.UplinkFrame) {
	gatewayLocalID, err := utils.BytesToGatewayID(frame.GetRxInfo().GetGatewayId())
	if err != nil {
		logrus.WithError(err).Warn("received uplink from gateway with invalid gateway ID")
		return
	}

	log := logrus.WithField("gw_local_id", gatewayLocalID)

	// ensure that received frame is from a trusted gateway if not drop it
	gw, ok := e.trustedGateways.ByLocalIDBytes(frame.RxInfo.GatewayId)
	if !ok {
		uplinksCounter.WithLabelValues(lorawan.EUI64{}.String(), "failed").Inc()
		log.Warn("uplink from unknown gateway, drop packet")
		e.recordUnknownGateway(gatewayLocalID)
		return
	}

	// log frame details
	log = log.WithField("gw_network_id", gw.NetworkGatewayID)
	frameLog := log.WithFields(logrus.Fields{
		"rssi":        frame.GetRxInfo().GetRssi(),
		"snr":         frame.GetRxInfo().GetLoraSnr(),
		"freq":        frame.GetTxInfo().GetFrequency(),
		"sf":          frame.GetTxInfo().GetLoraModulationInfo().GetSpreadingFactor(),
		"pol":         frame.GetTxInfo().GetLoraModulationInfo().GetPolarizationInversion(),
		"coderate":    frame.GetTxInfo().GetLoraModulationInfo().GetCodeRate(),
		"payload":     base64.RawStdEncoding.EncodeToString(frame.GetPhyPayload()),
		"payload_len": len(frame.GetPhyPayload()),
	})

	// convert the frame from its local format (gateway <-> exchange) into its network
	// representation (exchange <-> router) so it can be broadcasted onto the network
	if frame, err = localUplinkFrameToNetwork(gw, frame); err != nil {
		uplinksCounter.WithLabelValues(gw.NetworkGatewayID.String(), "failed").Inc()
		frameLog.WithError(err).Error("update uplink frame to network format failed, drop packet")
		return
	}

	// decode it into a lorawan packet to determine what needs to be done
	var phy lorawan.PHYPayload
	if err := phy.UnmarshalBinary(frame.PhyPayload); err != nil {
		uplinksCounter.WithLabelValues(gw.NetworkGatewayID.String(), "failed").Inc()
		frameLog.WithError(err).Error("could not decode lorawan packet, drop packet")
		return
	}

	airtime, _ := airtime.UplinkAirtime(frame)

	frameLog = frameLog.WithFields(logrus.Fields{
		"type":    phy.MHDR.MType,
		"airtime": airtime,
	})

	switch phy.MHDR.MType {
	case lorawan.ConfirmedDataUp, lorawan.UnconfirmedDataUp:
		// Filter by NetID
		mac, ok := phy.MACPayload.(*lorawan.MACPayload)
		if !ok {
			uplinksCounter.WithLabelValues(gw.NetworkGatewayID.String(), "failed").Inc()
			frameLog.Error("invalid packet: data-up but no mac-payload, drop packet")
			return
		}
		frameLog = frameLog.WithFields(logrus.Fields{
			"dev_addr": mac.FHDR.DevAddr,
			"fcnt":     mac.FHDR.FCnt,
		})

		// TODO: Handle mapper mac and forward to mapping service
		// if the packet was send by a mapper forward it to the mapper service
		if IsMaybeMapperPacket(mac) {
			frameLog.Warn("TODO: process received mapper packet (skip for now)")
			//fw.mapperClient.HandleMapperPacket(frame, mac)
			return
		}

		event := router.GatewayToRouterEvent{
			GatewayInformation: &router.GatewayInformation{
				PublicKey: gw.CompressedPubKeyBytes(),
				Owner:     gw.Owner.Bytes(),
			},
			Event: &router.GatewayToRouterEvent_UplinkFrameEvent{
				UplinkFrameEvent: &router.UplinkFrameEvent{
					UplinkFrame: &frame,
					AirtimeReceipt: &router.AirtimeReceipt{
						Owner:   gw.Owner.Bytes(),
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
			uplinksCounter.WithLabelValues(gw.NetworkGatewayID.String(), "failed").Inc()
			frameLog.Warn("unable to broadcast uplink to routing table, drop packet")
		} else {
			uplinksCounter.WithLabelValues(gw.NetworkGatewayID.String(), "success").Inc()
			frameLog.Info("delivered packet")
		}
	case lorawan.JoinRequest, lorawan.RejoinRequest:
		// Filter by Xor8 filter on joinEUI
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
				Owner:     gw.Owner.Bytes(),
			},
			Event: &router.GatewayToRouterEvent_UplinkFrameEvent{
				UplinkFrameEvent: &router.UplinkFrameEvent{
					UplinkFrame: &frame,
					AirtimeReceipt: &router.AirtimeReceipt{
						Owner:   gw.Owner.Bytes(),
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
				joinEUI lorawan.EUI64
				event   *router.GatewayToRouterEvent
			}{
				jr.JoinEUI, &event,
			},
		}) {
			uplinksCounter.WithLabelValues(gw.NetworkGatewayID.String(), "failed").Inc()
			frameLog.Warn("unable to broadcast uplink to routing table, drop packet")
		} else {
			uplinksCounter.WithLabelValues(gw.NetworkGatewayID.String(), "success").Inc()
			frameLog.Info("delivered packet")
		}
	}
}

func (e *Exchange) gatewayStats(stats gw.GatewayStats) {
	gw, ok := e.trustedGateways.ByLocalIDBytes(stats.GetGatewayId())
	if !ok {
		logrus.Warn("gateway stats from unknown gateway, drop stats")
		return
	}

	var (
		id, _ = uuid.FromBytes(stats.StatsId)
		log   = logrus.WithFields(logrus.Fields{
			"gw_network_id": gw.NetworkGatewayID,
			"gw_local_id":   gw.LocalGatewayID,
			"id":            id,
		})
		gatewayNetworkID = gw.NetworkGatewayID.String()
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
			lora.GetCodeRate()).Set(float64(m.GetCount()))
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
			lora.GetCodeRate()).Set(float64(m.GetCount()))
	}

	for status, count := range stats.TxPacketsPerStatus {
		gatewayTxPacketsPerStatusGauge.WithLabelValues(gatewayNetworkID, status).Set(float64(count))
	}

	gatewayTxPacketsReceivedGauge.WithLabelValues(gatewayNetworkID).Set(float64(stats.TxPacketsReceived))
}

var (
	onlineGateways mapset.Set[lorawan.EUI64]
)

func init() {
	onlineGateways = mapset.New[lorawan.EUI64]()
}

// subscribeEvent is called by the chirstack backend, currently only when a gateway
// is online this callback is called.ys
func (e *Exchange) subscribeEvent(event events.Subscribe) {
	log := logrus.WithField("gw_local_id", hex.EncodeToString(event.GatewayID[:]))
	localGatewayID, err := utils.BytesToGatewayID(event.GatewayID[:])
	if err != nil {
		log.Warn("event from gateway with invalid local id, drop event")
		return
	}
	// ensure that received frame is from a trusted gateway if not drop it
	gw, ok := e.trustedGateways.ByLocalIDBytes(event.GatewayID[:])
	if !ok {
		log.Warn("event from unknown gateway, drop event")
		e.recordUnknownGateway(localGatewayID)
		return
	}

	log = log.WithField("gw_network_id", gw.NetworkGatewayID)
	emitEvents := false

	if event.Subscribe {
		if !onlineGateways.Has(gw.LocalGatewayID) {
			log.Info("gateway online")
			emitEvents = true
		}
		onlineGateways.Put(gw.LocalGatewayID)
	} else {
		if onlineGateways.Has(gw.LocalGatewayID) {
			log.Info("gateway offline")
			emitEvents = true
		}
		onlineGateways.Remove(gw.LocalGatewayID)
	}

	// event is valid, router clients are subscribed to this uplink broadcaster
	// and will receive it. If the router they are connected to is interested in
	// the package it will send the packet to the router.
	if emitEvents {
		if !e.routingTable.gatewayEvents.TryBroadcast(&GatewayEvent{
			receivedFrom: gw,
			subOnlineOfflineEvent: &struct {
				event *router.GatewayToRouterEvent
			}{
				&router.GatewayToRouterEvent{
					GatewayInformation: &router.GatewayInformation{
						PublicKey: gw.CompressedPubKeyBytes(),
						Owner:     gw.Owner.Bytes(),
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
}

func (e *Exchange) handleDownlinkFrame(event *router.DownlinkFrameEvent) {
	var (
		frame        = event.GetDownlinkFrame()
		gwNetworkId  = GatewayIDBytesToLoraEUID(frame.GetGatewayId())
		log          = logrus.WithField("gw_network_id", gwNetworkId)
		sink, sinkOk = e.trustedGateways.byNetworkID[gwNetworkId]
	)

	if !sinkOk {
		downlinksCounter.WithLabelValues(gwNetworkId.String(), "failed").Inc()
		log.WithFields(logrus.Fields{
			"payload": base64.RawStdEncoding.EncodeToString(frame.GetPhyPayload()),
		}).Warn("drop downlink frame - target gateway not found")
		return
	}

	log = log.WithField("gw_local_id", sink.LocalGatewayID)

	// convert the network downlink frame into a local frame
	frame = networkDownlinkFrameToLocal(sink, frame)

	// order backend to send the downlink to the gateway so it can be broadcasted
	if err := e.backend.SendDownlinkFrame(*frame); err != nil {
		downlinksCounter.WithLabelValues(gwNetworkId.String(), "failed").Inc()
		log.WithError(err).Error("drop downlink: unable to send to gateway")
		return
	} else {
		downlinksCounter.WithLabelValues(gwNetworkId.String(), "ok").Inc()
		log.Info("downlink sent to backend")
	}
}

func (e *Exchange) downlinkTxAck(txack gw.DownlinkTXAck) {
	var (
		log = logrus.WithFields(logrus.Fields{
			"gw_local_id": hex.EncodeToString(txack.GatewayId),
		})
	)
	log.Info("received downlink tx ack from gateway")

	localGatewayID, err := utils.BytesToGatewayID(txack.GetGatewayId())
	if err != nil {
		log.Error("received downlink ACK with gateway local id, drop packet")
		return
	}

	// ensure that received frame is from a trusted gateway if not drop it
	gw, ok := e.trustedGateways.ByLocalID(localGatewayID)
	if !ok {
		downlinkTxAckCounter.WithLabelValues(fmt.Sprintf("%x", txack.GetGatewayId()), "failed").Inc()
		log.Warn("downlink tx ack from unknown gateway, drop packet")
		e.recordUnknownGateway(localGatewayID)
		return
	}
	log = log.WithField("gw_network_id", gw.NetworkGatewayID)

	// convert txack to network format
	if txack, err = localDownlinkTxAckToNetwork(gw, txack); err != nil {
		downlinkTxAckCounter.WithLabelValues(fmt.Sprintf("%x", txack.GetGatewayId()), "failed").Inc()
		logrus.WithError(err).Errorf("could update txack to network format")
		return
	}

	event := router.GatewayToRouterEvent{
		GatewayInformation: &router.GatewayInformation{
			PublicKey: gw.CompressedPubKeyBytes(),
			Owner:     gw.Owner.Bytes(),
		},
		Event: &router.GatewayToRouterEvent_DownlinkTXAckEvent{
			DownlinkTXAckEvent: &router.DownlinkTXAckEvent{
				DownlinkTXAck: &txack,
				AirtimeReceipt: &router.AirtimeReceipt{
					Owner: gw.Owner.Bytes(),
					//TODO: Airtime: uint32(airtime.Milliseconds()),
				},
			},
		},
	}

	// send downlink ACK to the routing table that will forward it to the router
	if !e.routingTable.gatewayEvents.TryBroadcast(&GatewayEvent{
		receivedFrom: gw,
		downlinkAck: &struct {
			downlinkID []byte
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

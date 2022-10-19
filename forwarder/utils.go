package forwarder

import (
	"fmt"

	"github.com/ThingsIXFoundation/definitions-go"
	gateway_registry "github.com/ThingsIXFoundation/gateway-registry-go"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/sirupsen/logrus"
)

// localUplinkFrameToNetwork converts the given frame that was received from a gateway
// into a frame that can be send anto the network on behalf of the given gw.
func localUplinkFrameToNetwork(gw *gateway.Gateway, frame gw.UplinkFrame) (gw.UplinkFrame, error) {
	copy(frame.RxInfo.GatewayId, gw.NetworkGatewayID[:])
	return frame, nil
}

func localDownlinkTxAckToNetwork(gw *gateway.Gateway, txack gw.DownlinkTXAck) (gw.DownlinkTXAck, error) {
	txack.GatewayId = gw.NetworkGatewayID[:]
	return txack, nil
}

func GatewayIDBytesToLoraEUID(id []byte) lorawan.EUI64 {
	var lid lorawan.EUI64
	copy(lid[:], id)
	return lid
}

func networkDownlinkFrameToLocal(gw *gateway.Gateway, frame *gw.DownlinkFrame) *gw.DownlinkFrame {
	frame.GatewayId = gw.LocalGatewayID[:]
	if frame.TxInfo != nil {
		frame.TxInfo.GatewayId = gw.LocalGatewayID[:]
	}
	for i := range frame.Items {
		frame.Items[i].TxInfo.GatewayId = gw.LocalGatewayID[:]
	}
	return frame
}

func loadGatewayStore(cfg *Config) (gateway.GatewayStore, error) {
	var (
		store gateway.GatewayStore
		err   error
	)

	if cfg.Gateways.YamlStorePath != nil {
		logrus.WithField("path", *cfg.Gateways.YamlStorePath).Info("use gateway store")
		if store, err = gateway.LoadGatewayYamlFileStore(*cfg.Gateways.YamlStorePath); err != nil {
			logrus.WithError(err).Fatal("unable to load gateway store")
		}
	} else {
		store = &gateway.GatewayYamlFileStore{} // use an empty store
		logrus.Warn("no gateway store configured")
	}

	return store, err
}

func onboardedAndRegisteredGateways(cfg *Config, store gateway.GatewayStore) (map[lorawan.EUI64]*gateway.Gateway, map[lorawan.EUI64]*gateway.Gateway, error) {
	if cfg.Gateways.RegistryAddress == nil {
		return nil, nil, fmt.Errorf("missing gateway registry address in configuration")
	}

	var (
		trustedGatewaysByLocalID   = make(map[lorawan.EUI64]*gateway.Gateway)
		trustedGatewaysByNetworkID = make(map[lorawan.EUI64]*gateway.Gateway)
	)

	client, err := ethclient.Dial(cfg.BlockChain.Endpoint)
	if err != nil {
		logrus.WithError(err).Error("unable to dial blockchain RPC node")
	}
	defer client.Close()

	registry, err := gateway_registry.NewGatewayRegistry(*cfg.Gateways.RegistryAddress, client)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to instantiate gateway registry bindings")
	}

	for _, gateway := range store.Gateways() {
		// forwarder only forwards data for gateways that are onboarded and
		// their details such as location are set in the registry. If not print
		// a warning and ignore the gateway.

		rgw, err := registry.Gateways(nil, gateway.ID())
		if err != nil {
			logrus.WithError(err).Error("unable to retrieve gateway details from registry")
			continue
		}

		if rgw.AntennaGain != 0 {
			gateway.Owner = rgw.Owner
			trustedGatewaysByLocalID[gateway.LocalGatewayID] = gateway
			trustedGatewaysByNetworkID[gateway.NetworkGatewayID] = gateway
			logrus.WithFields(logrus.Fields{
				"local-id":     gateway.LocalGatewayID,
				"network-id":   gateway.NetworkGatewayID,
				"location":     fmt.Sprintf("%x", rgw.Location),
				"altitude":     rgw.Altitude / 3,
				"antenna-gain": fmt.Sprintf("%.1f", (float32(rgw.AntennaGain) / 10.0)),
				"owner":        gateway.Owner,
				"freq-plan":    definitions.FrequencyPlan(rgw.FrequencyPlan),
			}).Info("loaded gateway from store")
		} else {
			l := logrus.WithFields(logrus.Fields{
				"id":         fmt.Sprintf("%x", gateway.ID()),
				"local_id":   gateway.LocalGatewayID,
				"network_id": gateway.NetworkGatewayID,
			})
			if rgw.Owner != (common.Address{}) {
				l.Warn("ingore gateway, details not set in gateway registry")
			} else {
				l.Warn("ignore gateway, gateway not onboarded and details not set in gateway registry")
			}
		}
	}

	return trustedGatewaysByLocalID, trustedGatewaysByNetworkID, nil
}

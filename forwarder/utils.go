package forwarder

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/ThingsIXFoundation/definitions-go"
	gateway_registry "github.com/ThingsIXFoundation/gateway-registry-go"
	"github.com/ThingsIXFoundation/packet-handling/gateway"
	router_registry "github.com/ThingsIXFoundation/router-registry-go"
	"github.com/brocaar/chirpstack-api/go/v3/gw"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
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

	if cfg.Forwarder.Gateways.Store.YamlStorePath != nil {
		logrus.WithField("path", *cfg.Forwarder.Gateways.Store.YamlStorePath).Info("use gateway store")
		if store, err = gateway.LoadGatewayYamlFileStore(*cfg.Forwarder.Gateways.Store.YamlStorePath); err != nil {
			logrus.WithError(err).Fatal("unable to load gateway store")
		}
	} else {
		// no gateway store configured, fallback to default yaml gateway store
		// in $HOME/gateway-store.yaml
		home, err := os.UserHomeDir()
		if err != nil {
			logrus.Fatal("no gateway store configured")
		}
		storePath := path.Join(home, "gateway-store.yaml")
		logrus.WithField("path", storePath).Warn("no gateway store configured, use file based store")
		if store, err = gateway.LoadGatewayYamlFileStore(storePath); err != nil {
			logrus.WithError(err).Fatal("unable to load gateway store")
		}
	}

	return store, err
}

func acceptOnlyOnboardedAndRegistryGateways(cfg *Config, gateways []*gateway.Gateway) (map[lorawan.EUI64]*gateway.Gateway, map[lorawan.EUI64]*gateway.Gateway, error) {
	client, err := ethclient.Dial(cfg.BlockChain.Polygon.Endpoint)
	if err != nil {
		logrus.WithError(err).Error("unable to dial blockchain RPC node")
	}
	defer client.Close()

	registry, err := gateway_registry.NewGatewayRegistry(*cfg.Forwarder.Gateways.RegistryAddress, client)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to instantiate gateway registry bindings")
	}

	var (
		trustedGatewaysByLocalID   = make(map[lorawan.EUI64]*gateway.Gateway)
		trustedGatewaysByNetworkID = make(map[lorawan.EUI64]*gateway.Gateway)
	)

	for _, gateway := range gateways {
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
				"altitude":     rgw.Altitude * 3,
				"antenna-gain": fmt.Sprintf("%.1f", (float32(rgw.AntennaGain) / 10.0)),
				"owner":        gateway.Owner,
				"freq-plan":    definitions.FrequencyPlan(rgw.FrequencyPlan),
			}).Debug("loaded gateway from store")
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

	return trustedGatewaysByLocalID, trustedGatewaysByNetworkID, err
}

func onboardedAndRegisteredGateways(cfg *Config, store gateway.GatewayStore) (map[lorawan.EUI64]*gateway.Gateway, map[lorawan.EUI64]*gateway.Gateway, error) {
	// If gateway registry is not configured accept data from all gateways from the store.
	// This is temporary until gateway onboards are made possible and ThingsIX moves from
	// data-only to a network with rewards.
	acceptOnlyRegisteredGateways := cfg.Forwarder.Gateways.RegistryAddress != nil

	if !acceptOnlyRegisteredGateways {
		logrus.Warn("accept all gateways in gateway store, including non-registered gateways")
		var (
			trustedGatewaysByLocalID   = make(map[lorawan.EUI64]*gateway.Gateway)
			trustedGatewaysByNetworkID = make(map[lorawan.EUI64]*gateway.Gateway)
		)

		for _, gateway := range store.Gateways() {
			trustedGatewaysByLocalID[gateway.LocalGatewayID] = gateway
			trustedGatewaysByNetworkID[gateway.NetworkGatewayID] = gateway
		}

		return trustedGatewaysByLocalID, trustedGatewaysByNetworkID, nil
	}

	return acceptOnlyOnboardedAndRegistryGateways(cfg, store.Gateways())
}

func fetchRoutersFromChain(cfg *Config, accounter Accounter) (RoutesUpdaterFunc, time.Duration, error) {
	interval := 30 * time.Minute // default refresh interval
	if cfg.Forwarder.Routers.OnChain.UpdateInterval != nil {
		if *cfg.Forwarder.Routers.OnChain.UpdateInterval < time.Minute {
			logrus.Warn("router on chain update interval too small, fall back to 30m")
		} else {
			interval = *cfg.Forwarder.Routers.OnChain.UpdateInterval
		}
	}

	logrus.WithField("interval", interval).Info("retrieve routers on chain")

	return func() ([]*Router, error) {
		client, err := dialRPCNode(cfg)
		if err != nil {
			return nil, err
		}
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// determine latest confirmed block
		head, err := client.HeaderByNumber(ctx, nil)
		if err != nil {
			return nil, fmt.Errorf("unable to determine chain head: %w", err)
		}

		if head.Number.Uint64() < cfg.BlockChain.Polygon.Confirmations {
			return nil, nil // no confirmed blocks yet
		}

		var (
			confirmedBlock = head.Number.Uint64() - cfg.BlockChain.Polygon.Confirmations
			callOpts       = &bind.CallOpts{
				BlockNumber: new(big.Int).SetUint64(confirmedBlock),
			}
		)

		registry, err := router_registry.NewRouterRegistryCaller(cfg.Forwarder.Routers.OnChain.RegistryContract, client)
		if err != nil {
			return nil, fmt.Errorf("unable to instantiate router registry bindings")
		}

		routerCount, err := registry.RouterCount(callOpts)
		if err != nil {
			return nil, fmt.Errorf("unable to determine router count: %w", err)
		}

		var (
			routers  []*Router
			pageSize = int64(50)
		)
		for i := int64(0); i*pageSize < routerCount.Int64(); i += pageSize {
			fetchedRouters, err := registry.RoutersPaged(callOpts, big.NewInt(i), big.NewInt(i+pageSize))
			if err != nil {
				return nil, fmt.Errorf("unable to retrieve routers from registry: %w", err)
			}

			for _, r := range fetchedRouters {
				netids := make([]lorawan.NetID, len(r.Networks))
				for i, id := range r.Networks {
					var netid [4]byte
					binary.LittleEndian.PutUint32(netid[:], uint32(id.Uint64()))
					netids[i] = lorawan.NetID{netid[0], netid[1], netid[2]}
				}
				routers = append(routers, NewRouter(r.Id, r.Endpoint, false, netids, r.Owner, accounter))
			}
		}

		return routers, nil
	}, interval, nil
}

func dialRPCNode(cfg *Config) (*ethclient.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := ethclient.DialContext(ctx, cfg.BlockChain.Polygon.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("unable to dial RPC node: %w", err)
	}

	// ensure connected to the expected chain
	chainID, err := client.ChainID(ctx)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("unable to determine if dial RPC node on the correct network")
	}
	if chainID.Uint64() != cfg.BlockChain.Polygon.ChainID {
		return nil, fmt.Errorf("RPC node connected to wrong chain, want %d, got %d", cfg.BlockChain.Polygon.ChainID, chainID)
	}

	return client, nil
}

func fetchRoutersFromThingsIXAPI(cfg *Config, accounter Accounter) (RoutesUpdaterFunc, time.Duration, error) {
	interval := 30 * time.Minute // default refresh interval
	if cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval != nil {
		if *cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval < (15 * time.Minute) {
			logrus.Warn("router ThingsIX update interval too small, fall back to 30m")
		} else {
			interval = *cfg.Forwarder.Routers.ThingsIXApi.UpdateInterval
		}
	}

	logrus.WithField("interval", interval).Info("retrieve routers from ThingsIX API")

	return func() ([]*Router, error) {
		resp, err := http.Get(*cfg.Forwarder.Routers.ThingsIXApi.Endpoint)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		snapshot := struct {
			BlockNumber uint64
			ChainID     uint64 `json:"chainId"`
			Routers     []struct {
				Endpoint string
				ID       string
				Owner    common.Address
				Networks []uint32
			}
		}{}

		if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
			return nil, err
		}
		if snapshot.ChainID != cfg.BlockChain.Polygon.ChainID {
			return nil, fmt.Errorf("router snapshot from wrong chain, got %d, want %d", snapshot.ChainID, cfg.BlockChain.Polygon.ChainID)
		}

		// convert from snapshot to internal format
		routers := make([]*Router, len(snapshot.Routers))
		for i, r := range snapshot.Routers {
			var (
				id     [32]byte
				netids = make([]lorawan.NetID, len(r.Networks))
			)
			rID, err := hex.DecodeString(r.ID)
			if err != nil {
				logrus.WithError(err).Error("unable to decode router id")
				continue
			}

			copy(id[:], rID)
			for i, id := range r.Networks {
				var netid [4]byte
				binary.LittleEndian.PutUint32(netid[:], id)
				netids[i] = lorawan.NetID{netid[0], netid[1], netid[2]}
			}
			routers[i] = NewRouter(id, r.Endpoint, false, netids, r.Owner, accounter)
		}
		logrus.WithField("#routers", len(routers)).Info("fetched routing table from ThingsIX API")
		return routers, nil
	}, interval, nil
}

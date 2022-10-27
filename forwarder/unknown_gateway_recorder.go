package forwarder

import (
	"os"
	"sync"

	"github.com/brocaar/lorawan"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

type UnknownGatewayLoggerFunc func(localGatewayID lorawan.EUI64)

// NewUnknownGatewayLogger returns a callback that can be used to record gateways their
// local id to a source defined in the given cfg. This is used to record unknown gateways
// that connected to the backend. These can be verified later and if required imported
// into the gateway store and registered on ThingsIX.
func NewUnknownGatewayLogger(cfg *Config) UnknownGatewayLoggerFunc {
	if cfg.Forwarder.Gateways.RecordUnknown != nil && cfg.Forwarder.Gateways.RecordUnknown.File != "" {
		return recordUnkownGatewaysToFile(cfg)
	}

	return func(lorawan.EUI64) {} // user has not configured to record unknown gateways
}

// recordUnkownGatewaysToFile uses a yaml file to record gateway local id's for
// gateways that are connected but not in the forwarders gateway store. This can
// be used to easily add new gateways.
func recordUnkownGatewaysToFile(cfg *Config) UnknownGatewayLoggerFunc {
	var (
		outputfile       = cfg.Forwarder.Gateways.RecordUnknown.File
		log              = logrus.WithField("file", outputfile)
		gatewaysMu       sync.Mutex
		recordedGateways = make(map[lorawan.EUI64]struct{})
		recordID         = make(chan lorawan.EUI64, 16)
	)

	log.Info("record unknown gateways to file")

	// read already recorded gateways from file
	file, err := os.Open(outputfile)
	if err == nil {
		var alreadyRecorded []lorawan.EUI64
		if err := yaml.NewDecoder(file).Decode(&alreadyRecorded); err != nil {
			log.WithError(err).Fatal("unable to load existint set of recorded gateways")
		}
		file.Close()
		for _, id := range alreadyRecorded {
			recordedGateways[id] = struct{}{}
		}
	} else if !os.IsNotExist(err) {
		log.WithError(err).
			Fatal("unable to read recorded unknown gateways from file")
	}

	// write new gateway id in the background
	go func() {
		for {
			id, ok := <-recordID
			if !ok {
				return
			}
			bytes, _ := yaml.Marshal([]lorawan.EUI64{id})
			file, err := os.OpenFile(outputfile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0666)
			if err != nil {
				log.WithError(err).Error("unable to open unknown gateway record file")
				continue
			}
			file.Write(bytes)
			file.Close()

			log.WithField("gw_local_id", id).Info("unknown gateway recorded")
		}
	}()

	// return callback to record connected gateways that are not included in the store
	return func(localGatewayID lorawan.EUI64) {
		gatewaysMu.Lock()
		defer gatewaysMu.Unlock()

		if _, ok := recordedGateways[localGatewayID]; !ok {
			recordedGateways[localGatewayID] = struct{}{}
			go func() {
				recordID <- localGatewayID
			}()
		}
	}
}

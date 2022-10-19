package forwarder

import (
	"fmt"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/semtechudp"
	chirpconfig "github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/config"
	"github.com/sirupsen/logrus"
)

func buildBackend(cfg ExchangeGatewayBackendConfig) (Backend, error) {
	switch cfg.Type() {
	case SemtechUDPBackendType:
		semtechCfg, err := cfg.SemtechUDPConfig()
		if err != nil {
			return nil, fmt.Errorf("invalid semtech backend configuration: %w", err)
		}
		var chirpCfg chirpconfig.Config
		chirpCfg.Backend.Type = string(SemtechUDPBackendType)
		chirpCfg.Backend.SemtechUDP.UDPBind = semtechCfg.UDPBind
		chirpCfg.Backend.SemtechUDP.FakeRxTime = semtechCfg.FakeRxTime
		chirpCfg.Backend.SemtechUDP.SkipCRCCheck = semtechCfg.SkipCRCCheck

		logrus.WithFields(logrus.Fields{
			"udp_bind":       chirpCfg.Backend.SemtechUDP.UDPBind,
			"skip_crc_check": chirpCfg.Backend.SemtechUDP.SkipCRCCheck,
			"fake_rx_time":   chirpCfg.Backend.SemtechUDP.FakeRxTime,
		}).Info("use semtech udp backend")

		backend, err := semtechudp.NewBackend(chirpCfg)
		if err != nil {
			return nil, fmt.Errorf("unable to instantiate semtech backend: %w", err)
		}
		return backend, nil
	case BasicStationBackendType, ConcentratorDBackendType:
		return nil, fmt.Errorf("backend '%s' not (yet) supported", cfg.Type())
	}

	return nil, fmt.Errorf("unsupported backend '%s'", cfg.Type())
}

package forwarder

import (
	"fmt"

	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/backend/semtechudp"
	chirpconfig "github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/config"
	"github.com/sirupsen/logrus"
)

// buildBackend returns the forwarder Chirpstack backend that was configured
// in the given cfg. Or an error in case of missing configuration or invalid
// configuration.
func buildBackend(cfg *Config) (Backend, error) {
	switch {
	case cfg.Forwarder.Backend.SemtechUDP != nil:
		return buildSemtechUDPBackend(cfg)
	case cfg.Forwarder.Backend.BasicStation != nil:
		return nil, fmt.Errorf("backend basic station not supported")
	case cfg.Forwarder.Backend.Concentratord != nil:
		return nil, fmt.Errorf("backend concentratord not supported")
	default:
		return nil, fmt.Errorf("invalid backend configuration")
	}
}

// buildSemtechUDPBackend return the Chirpstack UDP backend implementation
// based on the given cfg.
func buildSemtechUDPBackend(cfg *Config) (*semtechudp.Backend, error) {
	var (
		chirpCfg     chirpconfig.Config
		udpBind      = "0.0.0.0:1680" // default
		fakeRxTime   = false          // default
		skipCRCCheck = false          // default
	)

	if cfg.Forwarder.Backend.SemtechUDP.UDPBind != nil {
		udpBind = *cfg.Forwarder.Backend.SemtechUDP.UDPBind
	}

	if cfg.Forwarder.Backend.SemtechUDP.FakeRxTime != nil {
		fakeRxTime = *cfg.Forwarder.Backend.SemtechUDP.FakeRxTime
	}

	if cfg.Forwarder.Backend.SemtechUDP.SkipCRCCheck != nil {
		skipCRCCheck = *cfg.Forwarder.Backend.SemtechUDP.SkipCRCCheck
	}

	chirpCfg.Backend.Type = "semtech_udp"
	chirpCfg.Backend.SemtechUDP.UDPBind = udpBind
	chirpCfg.Backend.SemtechUDP.FakeRxTime = fakeRxTime
	chirpCfg.Backend.SemtechUDP.SkipCRCCheck = skipCRCCheck

	logrus.WithFields(logrus.Fields{
		"udp_bind":       chirpCfg.Backend.SemtechUDP.UDPBind,
		"skip_crc_check": chirpCfg.Backend.SemtechUDP.SkipCRCCheck,
		"fake_rx_time":   chirpCfg.Backend.SemtechUDP.FakeRxTime,
	}).Info("Semtech UDP backend")

	backend, err := semtechudp.NewBackend(chirpCfg)
	if err != nil {
		return nil, fmt.Errorf("unable to instantiate semtech backend: %w", err)
	}
	return backend, nil
}

package router

import (
	"fmt"

	chirpconfig "github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/config"
	"github.com/ThingsIXFoundation/packet-handling/external/chirpstack/gateway-bridge/integration"
)

func buildIntegrations(cfg *Config) (integration.Integration, error) {
	if cfg.Router.Integration.MQTT != nil {
		return buildIntegrationsForMQTT(cfg.Router)
	}

	return nil, fmt.Errorf("missing router integrations configuration")
}

func buildIntegrationsForMQTT(cfg RouterConfig) (integration.Integration, error) {
	var chirpConfig chirpconfig.Config

	chirpConfig.Integration.MQTT.StateRetained = cfg.Integration.MQTT.StateRetained
	chirpConfig.Integration.MQTT.KeepAlive = cfg.Integration.MQTT.KeepAlive
	chirpConfig.Integration.MQTT.MaxReconnectInterval = cfg.Integration.MQTT.MaxReconnectInterval
	chirpConfig.Integration.MQTT.MaxTokenWait = cfg.Integration.MQTT.MaxTokenWait
	if cfg.Integration.MQTT.Auth != nil && cfg.Integration.MQTT.Auth.Generic != nil {
		chirpConfig.Integration.MQTT.Auth.Type = "generic"
		chirpConfig.Integration.MQTT.Auth.Generic.Servers = cfg.Integration.MQTT.Auth.Generic.Servers
		chirpConfig.Integration.MQTT.Auth.Generic.Username = cfg.Integration.MQTT.Auth.Generic.Username
		chirpConfig.Integration.MQTT.Auth.Generic.Password = cfg.Integration.MQTT.Auth.Generic.Password
	}
	if cfg.Integration.MQTT.Auth != nil && cfg.Integration.MQTT.Auth.GCPCloudIoTCore != nil {
		return nil, fmt.Errorf("GCP cloud IoT core auth unsupported")
	}
	if cfg.Integration.MQTT.Auth != nil && cfg.Integration.MQTT.Auth.AzureIoTHub != nil {
		return nil, fmt.Errorf("azure IoT hub auth unsupported")
	}

	if cfg.Integration.MQTT.EventTopicTemplate != "" {
		chirpConfig.Integration.MQTT.EventTopicTemplate = cfg.Integration.MQTT.EventTopicTemplate
	} else {
		chirpConfig.Integration.MQTT.EventTopicTemplate = "eu868/gateway/{{ .GatewayID }}/event/{{ .EventType }}"
	}

	if cfg.Integration.MQTT.StateTopicTemplate != "" {
		chirpConfig.Integration.MQTT.StateTopicTemplate = cfg.Integration.MQTT.StateTopicTemplate
	} else {
		chirpConfig.Integration.MQTT.StateTopicTemplate = "eu868/gateway/{{ .GatewayID }}/state/{{ .StateType }}"
	}

	if cfg.Integration.MQTT.CommandTopicTemplate != "" {
		chirpConfig.Integration.MQTT.CommandTopicTemplate = cfg.Integration.MQTT.CommandTopicTemplate
	} else {
		chirpConfig.Integration.MQTT.CommandTopicTemplate = "eu868/gateway/{{ .GatewayID }}/command/#"
	}

	chirpConfig.Integration.MQTT.Auth.Generic.CleanSession = cfg.Integration.MQTT.Auth.Generic.CleanSession

	chirpConfig.Integration.Marshaler = cfg.Integration.Marshaler

	if err := integration.Setup(chirpConfig); err != nil {
		return nil, err
	}

	return integration.GetIntegration(), nil
}

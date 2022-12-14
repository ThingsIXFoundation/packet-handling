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
		chirpConfig.Integration.MQTT.Auth.Generic.Server = cfg.Integration.MQTT.Auth.Generic.Server
		chirpConfig.Integration.MQTT.Auth.Generic.Servers = cfg.Integration.MQTT.Auth.Generic.Servers
		chirpConfig.Integration.MQTT.Auth.Generic.Username = cfg.Integration.MQTT.Auth.Generic.Username
		chirpConfig.Integration.MQTT.Auth.Generic.Password = cfg.Integration.MQTT.Auth.Generic.Password
		chirpConfig.Integration.MQTT.Auth.Generic.CACert = cfg.Integration.MQTT.Auth.Generic.CACert
		chirpConfig.Integration.MQTT.Auth.Generic.TLSCert = cfg.Integration.MQTT.Auth.Generic.TLSCert
		chirpConfig.Integration.MQTT.Auth.Generic.TLSKey = cfg.Integration.MQTT.Auth.Generic.TLSKey
		chirpConfig.Integration.MQTT.Auth.Generic.QOS = cfg.Integration.MQTT.Auth.Generic.QOS
		chirpConfig.Integration.MQTT.Auth.Generic.CleanSession = cfg.Integration.MQTT.Auth.Generic.CleanSession
		chirpConfig.Integration.MQTT.Auth.Generic.ClientID = cfg.Integration.MQTT.Auth.Generic.ClientID
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

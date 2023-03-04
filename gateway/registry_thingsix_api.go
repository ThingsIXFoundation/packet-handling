// Copyright 2023 Stichting ThingsIX Foundation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/sirupsen/logrus"
)

type GatewayThingsIXAPI struct {
	// Endpoint holds the ThingsIX API endpoint to retrieve gateway info
	Endpoint string
	// keeps track when the last time a gateway details where retrieved from the
	// ThingsIX registry API. If its too short return a cached version to
	// prevent the API to overflow with requests that very likely return the
	// same response.
	cache sync.Map
}

func buildThingsIXRegistryApiSyncer(cfg RegistrySyncAPIConfig) (*GatewayThingsIXAPI, error) {
	logrus.WithFields(logrus.Fields{
		"endpoint": cfg.Endpoint,
	}).Info("sync with ThingsIX gateway registry using API")

	return &GatewayThingsIXAPI{
		Endpoint: cfg.Endpoint,
		cache:    sync.Map{},
	}, nil
}

func (sync *GatewayThingsIXAPI) GatewayDetails(ctx context.Context, gatewayID ThingsIxID, force bool) (common.Address, uint8, *GatewayDetails, error) {
	// unless a new request is forced cache results for 10 minutes
	if !force {
		if v, ok := sync.cache.Load(gatewayID); ok {
			cached := v.(*cachedData)
			if cached.When.After(time.Now().Add(-10 * time.Minute)) {
				logrus.WithField("gateway", gatewayID).
					Debugf("ThingsIX API Gateway registry cache hit")
				return cached.Owner, cached.Version, cached.Details, cached.Err
			}
		}
	}

	endpoint := strings.Replace(sync.Endpoint, "{id}", gatewayID.String(), 1)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return common.Address{}, 0, nil, err
	}
	req.Header.Add("Accept", "application/json")
	req.Header.Add("User-Agent", fmt.Sprintf("ThingsIX forwarder :: %s", utils.Version()))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		sync.cache.Store(gatewayID, &cachedData{
			When: time.Now(),
			Err:  err,
		})
		return common.Address{}, 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		reply := struct {
			Altitude      uint16
			Version       uint8
			AntennaGain   float32
			FrequencyPlan string
			Location      string
			Owner         common.Address
		}{}
		if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
			sync.cache.Store(gatewayID, &cachedData{
				When: time.Now(),
				Err:  err,
			})
			return common.Address{}, 0, nil, fmt.Errorf("invalid API response")
		}

		// gateway onboarded but details are not set
		if reply.Owner != (common.Address{}) && reply.AntennaGain == 0 {
			sync.cache.Store(gatewayID, &cachedData{
				When:    time.Now(),
				Owner:   reply.Owner,
				Version: reply.Version,
				Err:     err,
			})
			return reply.Owner, reply.Version, nil, nil
		}

		band := reply.FrequencyPlan
		antennaGain := fmt.Sprintf("%.1f", reply.AntennaGain)
		sync.cache.Store(gatewayID, &cachedData{
			When:    time.Now(),
			Owner:   reply.Owner,
			Version: reply.Version,
			Details: &GatewayDetails{
				Altitude:    &reply.Altitude,
				AntennaGain: &antennaGain,
				Band:        &band,
				Location:    &reply.Location,
			},
			Err: err,
		})

		return reply.Owner, reply.Version, &GatewayDetails{
			Altitude:    &reply.Altitude,
			AntennaGain: &antennaGain,
			Band:        &band,
			Location:    &reply.Location,
		}, nil
	case http.StatusNotFound:
		return common.Address{}, 0, nil, ErrNotFound
	default:
		return common.Address{}, 0, nil, fmt.Errorf("unable to retrieve gateway details from ThingsIX API")
	}
}

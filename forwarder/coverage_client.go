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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/ThingsIXFoundation/coverage-api/go/mapper"
	h3light "github.com/ThingsIXFoundation/h3-light"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type CoverageClient struct {
	indexMutex sync.RWMutex
	// The index contains the coverage-mapping-service URL for each res1
	index map[h3light.Cell]string

	// The interval to fresh the index in
	indexRefreshInterval *time.Duration

	// The endpoint to fetch the index from
	indexEndpoint *string
}

func NewCoverageClient(cfg *Config) (*CoverageClient, error) {
	return &CoverageClient{
		indexMutex:           sync.RWMutex{},
		index:                make(map[h3light.Cell]string),
		indexEndpoint:        cfg.Forwarder.Mapping.ThingsIXApi.IndexEndpoint,
		indexRefreshInterval: cfg.Forwarder.Mapping.ThingsIXApi.UpdateInterval,
	}, nil
}

func (cc *CoverageClient) refreshCoverageMappingIndex() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if cc.indexEndpoint == nil {
		return fmt.Errorf("no index-endpoint defined, not refreshing index")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *cc.indexEndpoint, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid response status-code: %d", resp.StatusCode)
	}

	var index map[h3light.Cell]string
	err = json.NewDecoder(resp.Body).Decode(&index)
	if err != nil {
		return err
	}

	cc.indexMutex.Lock()
	cc.index = index
	cc.indexMutex.Unlock()

	logrus.Info("coverage-mapping-index refreshed")

	return nil
}

func (cc *CoverageClient) Run(ctx context.Context) {
	logrus.Info("coverage-mapping-index refresh started")
	err := cc.refreshCoverageMappingIndex()
	if err != nil {
		logrus.WithError(err).Error("error while running initial coverage-mapping-index refresh")
	}
	if cc.indexRefreshInterval == nil {
		logrus.Warn("coverage-mapping-index refresh interval is empty, not refreshing")
		return
	}

	for {
		select {
		case <-time.After(*cc.indexRefreshInterval):
			err := cc.refreshCoverageMappingIndex()
			if err != nil {
				logrus.WithError(err).Error("error while running coverage-mapping-index refresh")
			}
		case <-ctx.Done():
			logrus.Info("coverage-mapping-index refresh stopped")
			return
		}
	}
}

func (cc *CoverageClient) getCoverageMappingServiceUrlForRegion(region h3light.Cell) string {
	cc.indexMutex.RLock()
	defer cc.indexMutex.RUnlock()
	service, ok := cc.index[region]
	if !ok {
		return ""
	}
	return service
}

func (cc *CoverageClient) DeliverDiscoveryPacketReceipt(ctx context.Context, region h3light.Cell, dpr *mapper.DiscoveryPacketReceipt) (*mapper.DiscoveryPacketReceiptResponse, error) {
	b, err := proto.Marshal(dpr)
	if err != nil {
		return nil, err
	}

	service := cc.getCoverageMappingServiceUrlForRegion(region)
	if service == "" {
		return nil, fmt.Errorf("no coverage-mapping-service found for region: %s", region)
	}

	r, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/mapping/discovery", service), bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("received error from coverage-service: %s", string(b))
	}

	dprp := &mapper.DiscoveryPacketReceiptResponse{}

	err = proto.Unmarshal(b, dprp)
	if err != nil {
		return nil, err
	}

	return dprp, nil
}

func (cc *CoverageClient) DeliverDownlinkConfirmationPacketReceipt(ctx context.Context, region h3light.Cell, dpr *mapper.DownlinkConfirmationPacketReceipt) error {
	b, err := proto.Marshal(dpr)
	if err != nil {
		return err
	}

	service := cc.getCoverageMappingServiceUrlForRegion(region)
	if service == "" {
		return fmt.Errorf("no coverage-mapping-service found for region: %s", region)
	}

	r, err := http.NewRequestWithContext(ctx, "POST", fmt.Sprintf("%s/mapping/downlink", service), bytes.NewBuffer(b))
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("received error from coverage-service: %s", string(b))
	}

	return nil

}

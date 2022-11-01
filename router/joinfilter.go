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
	"context"
	"crypto/tls"
	"fmt"
	"github.com/FastFilter/xorfilter"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"sync"
	"time"
)

type JoinFilterGenerator interface {
	JoinFilter(ctx context.Context) (*router.JoinFilter, error)
	UpdateFilter(ctx context.Context) error
}

func NewJoinFilterGenerator(config RouterConfig) (JoinFilterGenerator, error) {
	if config.JoinFilterGenerator.ChirpStack.Target != "" {
		return newChirpstackGenerator(config)
	}
	return nil, fmt.Errorf("unknown JoinFilterGenerator")
}

type chirpstackGenerator struct {
	dsc api.DeviceServiceClient

	filterMutex sync.RWMutex
	filter      *xorfilter.Xor8
}

func (c *chirpstackGenerator) JoinFilter(ctx context.Context) (*router.JoinFilter, error) {
	c.filterMutex.RLock()
	defer c.filterMutex.RUnlock()
	if c.filter != nil {
		return &router.JoinFilter{Filter: &router.JoinFilter_Xor8{Xor8: &router.Xor8Filter{
			Seed:         c.filter.Seed,
			Blocklength:  c.filter.BlockLength,
			Fingerprints: c.filter.Fingerprints,
		}}}, nil
	} else {
		return &router.JoinFilter{Filter: &router.JoinFilter_Xor8{Xor8: nil}}, nil
	}
}

func (c *chirpstackGenerator) UpdateFilter(ctx context.Context) error {
	var (
		devEUIs    []uint64
		hasMore           = true
		limit      uint32 = 500
		processed  uint32 = 0
		totalCount uint32 = 0
	)

	for hasMore {
		resp, err := c.dsc.List(ctx, &api.ListDevicesRequest{
			Limit:  limit,
			Offset: processed,
		})
		if err != nil {
			return fmt.Errorf("error while getting devEUIs from chirpstack: %w", err)
		}

		processed += uint32(len(resp.Result))
		hasMore = len(resp.Result) >= int(limit)

		for _, dev := range resp.GetResult() {
			eui, err := utils.Eui64FromString(dev.DevEui)
			if err != nil {
				logrus.WithError(err).Warnf("could not parse DevEui from string: %s, skipping", dev.DevEui)
				continue
			}
			devEUIs = append(devEUIs, utils.Eui64ToUint64(*eui))
		}

		totalCount = resp.GetTotalCount()
	}

	filter, err := xorfilter.Populate(devEUIs)
	if err != nil {
		return fmt.Errorf("error while populating xor8 filter from devEUIs: %w", err)
	}

	c.filterMutex.Lock()
	c.filter = filter
	c.filterMutex.Unlock()

	logrus.Infof("processed %d / %d devEUIs from Chirpstack into Xor8 filter for joining", processed, totalCount)

	return nil

}

// Chirpstack JWT credentials that also work over (internal) non-secured connections
type chirpstackJwtCredentials struct {
	token string
}

func (j *chirpstackJwtCredentials) GetRequestMetadata(ctx context.Context, url ...string) (map[string]string, error) {
	return map[string]string{
		"authorization": "Bearer " + j.token,
	}, nil
}

func (j *chirpstackJwtCredentials) RequireTransportSecurity() bool {
	return false
}

func newChirpstackGenerator(config RouterConfig) (JoinFilterGenerator, error) {
	conf := config.JoinFilterGenerator.ChirpStack
	cg := &chirpstackGenerator{}

	dialOpts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithPerRPCCredentials(&chirpstackJwtCredentials{token: conf.APIKey}),
	}

	if conf.Insecure {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{})))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, conf.Target, dialOpts...)
	if err != nil {
		return nil, err
	}

	cg.dsc = api.NewDeviceServiceClient(conn)

	return cg, nil
}

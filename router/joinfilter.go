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
	"sync"
	"time"

	"github.com/FastFilter/xorfilter"
	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/ThingsIXFoundation/router-api/go/router"
	"github.com/chirpstack/chirpstack/api/go/v4/api"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
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
	asc api.ApplicationServiceClient
	tsc api.TenantServiceClient

	filterMutex sync.RWMutex
	filter      *xorfilter.Xor8
	bitmapBytes []byte
}

func (c *chirpstackGenerator) JoinFilter(ctx context.Context) (*router.JoinFilter, error) {
	c.filterMutex.RLock()
	defer c.filterMutex.RUnlock()
	if c.filter != nil {
		return &router.JoinFilter{
			Filter: &router.JoinFilter_Xor8{Xor8: &router.Xor8Filter{
				Seed:         c.filter.Seed,
				Blocklength:  c.filter.BlockLength,
				Fingerprints: c.filter.Fingerprints,
			}},
			RoaringBitmap: c.bitmapBytes,
		}, nil
	} else {
		return &router.JoinFilter{Filter: &router.JoinFilter_Xor8{Xor8: nil}}, nil
	}
}

func (c *chirpstackGenerator) getTenantIds(ctx context.Context) ([]string, error) {
	var (
		hasMore          = true
		limit     uint32 = 500
		processed uint32 = 0
		tenIds    []string
	)
	for hasMore {
		resp, err := c.tsc.List(ctx, &api.ListTenantsRequest{
			Limit:  limit,
			Offset: processed,
		})
		if err != nil {
			return nil, fmt.Errorf("got error while listing tenants: %w", err)
		}

		processed += uint32(len(resp.Result))
		hasMore = len(resp.Result) >= int(limit)

		for _, ten := range resp.GetResult() {
			tenIds = append(tenIds, ten.Id)
		}
	}

	return tenIds, nil
}

func (c *chirpstackGenerator) getApplicationIds(ctx context.Context, tenantId string) ([]string, error) {
	var (
		hasMore          = true
		limit     uint32 = 500
		processed uint32 = 0
		appIds    []string
	)
	for hasMore {
		resp, err := c.asc.List(ctx, &api.ListApplicationsRequest{
			TenantId: tenantId,
			Limit:    limit,
			Offset:   processed,
		})
		if err != nil {
			return nil, fmt.Errorf("got error while listing applications: %w", err)
		}

		processed += uint32(len(resp.Result))
		hasMore = len(resp.Result) >= int(limit)

		for _, app := range resp.GetResult() {
			appIds = append(appIds, app.Id)
		}
	}

	return appIds, nil
}

func (c *chirpstackGenerator) getDevEuisForApplication(ctx context.Context, appId string) ([]uint64, error) {
	var (
		devEUIs   []uint64
		hasMore          = true
		limit     uint32 = 500
		processed uint32 = 0
	)

	for hasMore {
		resp, err := c.dsc.List(ctx, &api.ListDevicesRequest{
			ApplicationId: appId,
			Limit:         limit,
			Offset:        processed,
		})
		if err != nil {
			return nil, fmt.Errorf("got error while listing devices: %w", err)
		}

		processed += uint32(len(resp.Result))
		hasMore = len(resp.Result) >= int(limit)

		for _, dev := range resp.GetResult() {
			devResp, err := c.dsc.Get(ctx, &api.GetDeviceRequest{
				DevEui: dev.DevEui,
			})

			if err != nil {
				logrus.WithError(err).Warnf("could not get device details for DevEUI: %s", dev.DevEui)
				continue
			}

			if devResp.GetDevice().IsDisabled {
				continue
			}

			eui, err := utils.Eui64FromString(dev.DevEui)
			if err != nil {
				logrus.WithError(err).Warnf("could not parse DevEUI from string: %s, skipping", dev.DevEui)
				continue
			}
			devEUIs = append(devEUIs, utils.Eui64ToUint64(eui))
		}
	}

	return devEUIs, nil
}

func (c *chirpstackGenerator) UpdateFilter(ctx context.Context) error {
	var (
		devEUIs []uint64
	)

	tenantIds, err := c.getTenantIds(ctx)
	if err != nil {
		return err
	}

	for _, tenantId := range tenantIds {

		appIds, err := c.getApplicationIds(ctx, tenantId)
		if err != nil {
			return err
		}

		for _, appId := range appIds {
			newDevEUIs, err := c.getDevEuisForApplication(ctx, appId)
			if err != nil {
				return err
			}

			devEUIs = append(devEUIs, newDevEUIs...)
		}
	}

	var filter *xorfilter.Xor8
	var bitmapBytes []byte
	if len(devEUIs) > 0 {
		filter, err = xorfilter.Populate(devEUIs)
		if err != nil {
			return fmt.Errorf("error while populating xor8 filter from devEUIs: %w", err)
		}
		bitmap := roaring64.New()
		bitmap.AddMany(devEUIs)
		bitmapBytes, err = bitmap.MarshalBinary()
		if err != nil {
			return fmt.Errorf("error while populating bitmap from devEUIs: %w", err)
		}
	} else {
		filter = nil
	}

	c.filterMutex.Lock()
	c.filter = filter
	c.bitmapBytes = bitmapBytes
	c.filterMutex.Unlock()

	logrus.Infof("processed %d devEUIs from ChirpStack into Xor8 filter and Bitmap of size %d kb for joining", len(devEUIs), len(bitmapBytes)/1024)

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
	cg.asc = api.NewApplicationServiceClient(conn)
	cg.tsc = api.NewTenantServiceClient(conn)

	return cg, nil
}

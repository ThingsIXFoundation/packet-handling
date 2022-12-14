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
	"io"
	"net/http"

	"github.com/ThingsIXFoundation/coverage-api/go/mapper"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type CoverageClient struct {
}

func NewCoverageClient() (*CoverageClient, error) {
	return &CoverageClient{}, nil
}

func (cc *CoverageClient) DeliverDiscoveryPacketReceipt(ctx context.Context, dpr *mapper.DiscoveryPacketReceipt) (*mapper.DiscoveryPacketReceiptResponse, error) {
	b, err := proto.Marshal(dpr)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, "POST", "http://localhost:8090/mapping/discovery", bytes.NewBuffer(b))
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, err
	}

	logrus.Debugf("got response from mapping server: %d", resp.StatusCode)

	dprp := &mapper.DiscoveryPacketReceiptResponse{}
	defer resp.Body.Close()
	b, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	err = proto.Unmarshal(b, dprp)
	if err != nil {
		return nil, err
	}

	return dprp, nil
}

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

package gateway

import (
	"errors"
	"fmt"
)

var (
	ErrStoreNotExists               = fmt.Errorf("gateway store doesn't exists")
	ErrNotFound                     = fmt.Errorf("not found")
	ErrServiceNotAvailable          = fmt.Errorf("service not available")
	ErrAlreadyExists                = fmt.Errorf("already exists")
	ErrInvalidConfig                = errors.New("invalid gateway store config")
	ErrInvalidGatewayID             = errors.New("invalid gateway id")
	ErrGatewayRegistryConfigMissing = errors.New("gateway ThingsIX registry config missing")
	ErrTooManySyncRequests          = errors.New("too fast gateway sync request")
)

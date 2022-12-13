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

// GatewayFiltererFunc is used by gateway stores to filter out gateways that
// are not (yet) suitable. For instance if these gateways have not yet been
// onboarded. If the gateway must be added to the in-memory gateway store and
// traffic must be exchanged between gateways and the ThingsIX network the
// filterer must return true for the given gw.
type GatewayFiltererFunc func(gw *Gateway) bool

// NoFilterer doesn't filter.
func NoGatewayFilterer(gw *Gateway) bool {
	return true
}

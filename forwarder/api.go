// Copyright 2023 Stichting ThingsIX Foundation
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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/sirupsen/logrus"
)

func runAPI(ctx context.Context, cfg *Config, store gateway.GatewayStore, unknownGateways gateway.UnknownGatewayLogger) {
	if cfg.Forwarder.Gateways.HttpAPI.Address == "" {
		logrus.Info("forwarder HTTP API disabled")
		return
	}

	root := chi.NewRouter()
	root.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	service := APIService{
		gateways:                store,
		chainID:                 new(big.Int).SetUint64(cfg.BlockChain.Polygon.ChainID),
		batchOnboarderAddress:   cfg.Forwarder.Gateways.BatchOnboarder.Address,
		unknown:                 unknownGateways,
		thingsIXOnboardEndpoint: cfg.Forwarder.Gateways.ThingsIXOnboardEndpoint,
	}

	logrus.WithFields(logrus.Fields{
		"chain_id":        service.chainID,
		"api_addr":        cfg.Forwarder.Gateways.HttpAPI.Address,
		"batch_onboarder": cfg.Forwarder.Gateways.BatchOnboarder.Address,
	}).Info("start forwarder HTTP API")

	root.Route("/v1", func(r chi.Router) {
		r.Route("/gateways", func(r chi.Router) {
			r.Post("/", service.AddGateway)
			r.Post("/onboard", service.OnboardGatewayMessage)
			r.Post("/import", service.ImportGateways)
			r.Get("/", service.ListGateways)
			r.Get("/unknown", service.ListUnknownGateways)
			r.Get("/{local_id}", service.Gateway)
			r.Get("/{local_id}/sync", service.SyncGateway)
		})
	})

	srv := http.Server{
		Handler:      root,
		Addr:         cfg.Forwarder.Gateways.HttpAPI.Address,
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	stopped := make(chan error)
	go func() {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		stopped <- srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logrus.WithError(err).Fatal("HTTP service crashed")
	}

	<-stopped
}

func replyJSON(w http.ResponseWriter, statusCode int, message interface{}) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(message)
}

type APIService struct {
	gateways                gateway.GatewayStore
	chainID                 *big.Int
	batchOnboarderAddress   common.Address
	unknown                 gateway.UnknownGatewayLogger
	thingsIXOnboardEndpoint string
}

func (svc APIService) AddGateway(w http.ResponseWriter, r *http.Request) {
	var (
		req struct {
			LocalID lorawan.EUI64 `json:"localId"`
		}
	)

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.LocalID == (lorawan.EUI64{}) {
		http.Error(w, "missing/invalid local id", http.StatusBadRequest)
		return
	}

	gw, err := svc.gateways.ByLocalID(req.LocalID)
	if err == nil {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(gw)
	} else if err != nil && errors.Is(err, gateway.ErrNotFound) {
		gw, err := gateway.GenerateNewGateway(req.LocalID)
		if err != nil {
			logrus.WithError(err).Error("unable to generate new gateway entry")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		gw, err = svc.gateways.Add(r.Context(), gw.LocalID, gw.PrivateKey)
		if err != nil {
			logrus.WithError(err).Error("unable to add new gateway entry to store")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(gw)
	} else if err != nil {
		logrus.WithError(err).Error("unable to determine if gateway is in store")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func (svc APIService) ImportGateways(w http.ResponseWriter, r *http.Request) {
	var (
		req struct {
			Owner          common.Address `json:"owner"`
			PushToThingsIX bool           `json:"pushToThingsIX"`
		}

		statusCode = http.StatusOK
		reply      = make([]OnboardGatewayReply, 0)
	)

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Owner == (common.Address{}) {
		http.Error(w, "missing owner", http.StatusBadRequest)
		return
	}

	recg, err := svc.unknown.Recorded()
	if err == nil {
		for _, rec := range recg {
			if !svc.gateways.ContainsByLocalID(rec.LocalID) {
				gw, err := gateway.GenerateNewGateway(rec.LocalID)
				if err != nil {
					logrus.WithError(err).Error("unable to generate new gateway entry")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}

				gw, err = svc.gateways.Add(r.Context(), gw.LocalID, gw.PrivateKey)
				if err != nil {
					logrus.WithError(err).Error("unable to add new gateway entry to store")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}

				signature, err := gateway.SignPlainBatchOnboardMessage(svc.chainID, svc.batchOnboarderAddress, req.Owner, 0, gw)
				if err != nil {
					logrus.WithError(err).Error("unable to sign onboard message")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}

				if req.PushToThingsIX {
					var (
						endpoint = strings.Replace(
							strings.Replace(svc.thingsIXOnboardEndpoint, "{owner}", strings.ToLower(req.Owner.String()), 1),
							"{onboarder}", strings.ToLower(svc.batchOnboarderAddress.String()), 1)
						payload, _ = json.Marshal(map[string]interface{}{
							"gatewayId":               gw.ID().String(),
							"gatewayOnboardSignature": fmt.Sprintf("0x%x", signature),
							"version":                 0,
							"localId":                 gw.LocalID.String(),
							"onboarder":               svc.batchOnboarderAddress,
						})
					)

					resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
					if err != nil {
						logrus.WithError(err).Error("unable to store gateway onboard message in ThingsIX")
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
						return
					}
					_ = resp.Body.Close()

					if resp.StatusCode == http.StatusCreated {
						logrus.WithField("id", gw.ID).Info("gateway onboard message pushed to ThingsIX")
					}
				}

				statusCode = http.StatusCreated

				reply = append(reply, OnboardGatewayReply{
					Owner:                   req.Owner,
					Address:                 gw.Address(),
					ChainID:                 svc.chainID.Uint64(),
					GatewayID:               gw.ID(),
					GatewayOnboardSignature: "0x" + hex.EncodeToString(signature),
					LocalID:                 gw.LocalID,
					NetworkID:               gw.NetworkID,
					Version:                 0,
					Onboarder:               svc.batchOnboarderAddress,
				})
			}
		}
		replyJSON(w, statusCode, reply)
	} else if errors.Is(err, gateway.ErrRecordingUnknownGatewaysDisabled) {
		http.Error(w, "recording unknown gateways disabled", http.StatusServiceUnavailable)
	} else {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (svc APIService) OnboardGatewayMessage(w http.ResponseWriter, r *http.Request) {
	var (
		req struct {
			LocalID        lorawan.EUI64  `json:"localId"`
			Owner          common.Address `json:"owner"`
			PushToThingsIX bool           `json:"pushToThingsIX"`
		}
		statusCode = http.StatusOK
	)

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.LocalID == (lorawan.EUI64{}) {
		http.Error(w, "missing/invalid local id", http.StatusBadRequest)
		return
	}

	if req.Owner == (common.Address{}) {
		http.Error(w, "missing owner", http.StatusBadRequest)
		return
	}

	// lookup gateway in store, if available sign onboard message with existing
	// key. If not, add gateway to store.
	gw, err := svc.gateways.ByLocalID(req.LocalID)
	if err != nil && errors.Is(err, gateway.ErrNotFound) {
		gw, err = gateway.GenerateNewGateway(req.LocalID)
		if err != nil {
			logrus.WithError(err).Error("unable to generate new gateway entry")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		gw, err = svc.gateways.Add(r.Context(), gw.LocalID, gw.PrivateKey)
		if err != nil {
			logrus.WithError(err).Error("unable to add new gateway entry to store")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		statusCode = http.StatusCreated
	} else if err != nil {
		logrus.WithError(err).Error("unable to determine if gateway is in store")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if gw.Owner != nil {
		logrus.WithField("owner", gw.Owner).Error("gateway already onboarded")
		reply := fmt.Sprintf("gateway already onboarded by %s", gw.Owner)
		http.Error(w, reply, http.StatusBadRequest)
		return
	}

	signature, err := gateway.SignPlainBatchOnboardMessage(svc.chainID, svc.batchOnboarderAddress, req.Owner, 0, gw)
	if err != nil {
		logrus.WithError(err).Error("unable to sign onboard message")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if req.PushToThingsIX {
		var (
			endpoint = strings.Replace(
				strings.Replace(svc.thingsIXOnboardEndpoint, "{owner}", strings.ToLower(req.Owner.String()), 1),
				"{onboarder}", strings.ToLower(svc.batchOnboarderAddress.String()), 1)
			payload, _ = json.Marshal(map[string]interface{}{
				"gatewayId":               gw.ID().String(),
				"gatewayOnboardSignature": fmt.Sprintf("0x%x", signature),
				"version":                 0,
				"localId":                 gw.LocalID.String(),
			})
		)

		resp, err := http.Post(endpoint, "application/json", bytes.NewReader(payload))
		if err != nil {
			logrus.WithError(err).Error("unable to store gateway onboard message in ThingsIX")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		_ = resp.Body.Close()

		if resp.StatusCode == http.StatusCreated {
			logrus.WithField("id", gw.ID).Info("gateway onboard message pushed to ThingsIX")
		}
	}

	replyJSON(w, statusCode, &OnboardGatewayReply{
		Owner:                   req.Owner,
		Address:                 gw.Address(),
		ChainID:                 svc.chainID.Uint64(),
		GatewayID:               gw.ID(),
		GatewayOnboardSignature: fmt.Sprintf("0x%x", signature),
		LocalID:                 gw.LocalID,
		NetworkID:               gw.NetworkID,
		Version:                 0,
		Onboarder:               svc.batchOnboarderAddress,
	})
}

type OnboardGatewayReply struct {
	Owner                   common.Address     `json:"owner"`
	Address                 common.Address     `json:"address"`
	ChainID                 uint64             `json:"chainId"`
	GatewayID               gateway.ThingsIxID `json:"gatewayId"`
	GatewayOnboardSignature string             `json:"gatewayOnboardSignature"`
	LocalID                 lorawan.EUI64      `json:"localId"`
	NetworkID               lorawan.EUI64      `json:"networkId"`
	Version                 uint8              `json:"version"`
	Onboarder               common.Address     `json:"onboarder"`
}

func (svc APIService) ListGateways(w http.ResponseWriter, r *http.Request) {
	var collector gateway.Collector
	svc.gateways.Range(&collector)

	var (
		pending   = make([]*gateway.Gateway, 0)
		onboarded = make([]*gateway.Gateway, 0)
	)

	for _, gw := range collector.Gateways {
		if gw.Onboarded() {
			onboarded = append(onboarded, gw)
		} else {
			pending = append(pending, gw)
		}
	}

	sort.Slice(pending, func(i, j int) bool {
		return bytes.Compare(pending[i].LocalID[:], pending[j].LocalID[:]) <= 0
	})
	sort.Slice(onboarded, func(i, j int) bool {
		return bytes.Compare(onboarded[i].LocalID[:], onboarded[j].LocalID[:]) <= 0
	})

	reply := map[string]interface{}{
		"pending":   pending,
		"onboarded": onboarded,
	}

	replyJSON(w, http.StatusOK, reply)
}

func (svc APIService) ListUnknownGateways(w http.ResponseWriter, r *http.Request) {
	recg, err := svc.unknown.Recorded()
	switch {
	case err == nil:
		filtered := make([]*gateway.RecordedUnknownGateway, 0)
		for _, r := range recg {
			if !svc.gateways.ContainsByLocalID(r.LocalID) {
				filtered = append(filtered, r)
			}
		}
		replyJSON(w, http.StatusOK, filtered)
	case errors.Is(err, gateway.ErrRecordingUnknownGatewaysDisabled):
		http.Error(w, "recording unknown gateways disabled", http.StatusServiceUnavailable)
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (svc APIService) Gateway(w http.ResponseWriter, r *http.Request) {
	localID, err := utils.Eui64FromString(chi.URLParam(r, "local_id"))
	if err != nil {
		http.Error(w, "invalid gateway local id", http.StatusBadRequest)
		return
	}

	gw, err := svc.gateways.ByLocalID(localID)
	switch {
	case err == nil:
		replyJSON(w, http.StatusOK, gw)
	case errors.Is(err, gateway.ErrNotFound):
		http.NotFound(w, r)
	default:
		logrus.WithError(err).Error("unable to retrieve gateway")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (svc APIService) SyncGateway(w http.ResponseWriter, r *http.Request) {
	localID, err := utils.Eui64FromString(chi.URLParam(r, "local_id"))
	if err != nil {
		http.Error(w, "invalid gateway local id", http.StatusBadRequest)
		return
	}
	gw, err := svc.gateways.SyncGatewayByLocalID(r.Context(), localID, true)
	switch {
	case err == nil:
		replyJSON(w, http.StatusOK, gw)
	case errors.Is(err, gateway.ErrNotFound):
		http.NotFound(w, r)
	default:
		logrus.WithError(err).Error("unable to sync gateway")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

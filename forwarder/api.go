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

	apiCtx := apiContext{
		gateways:              store,
		chainID:               new(big.Int).SetUint64(cfg.BlockChain.Polygon.ChainID),
		onboarderAddress:      cfg.Forwarder.Gateways.Onboarder.Address,
		batchOnboarderAddress: cfg.Forwarder.Gateways.BatchOnboarder.Address,
		unknown:               unknownGateways,
	}

	l := logrus.WithFields(logrus.Fields{
		"chain_id":  apiCtx.chainID,
		"api_addr":  cfg.Forwarder.Gateways.HttpAPI.Address,
		"onboarder": cfg.Forwarder.Gateways.Onboarder.Address,
	})
	if cfg.Forwarder.Gateways.Onboarder.Address != (common.Address{}) {
		l = l.WithField("onboarder", cfg.Forwarder.Gateways.Onboarder.Address)
	}

	l.Info("start forwarder HTTP API")

	root.Route("/v1", func(r chi.Router) {
		r.Route("/gateways", func(r chi.Router) {
			r.Post("/", withApiContext(AddGateway, &apiCtx))
			r.Post("/onboard", withApiContext(OnboardGatewayMessage, &apiCtx))
			r.Post("/import", withApiContext(ImportGateways, &apiCtx))
			r.Get("/", withApiContext(ListGateways, &apiCtx))
			r.Get("/unknown", withApiContext(ListUnknownGateways, &apiCtx))
			r.Get("/{local_id}", withApiContext(Gateway, &apiCtx))
			r.Get("/{local_id}/sync", withApiContext(SyncGateway, &apiCtx))
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

type apiContext struct {
	gateways              gateway.GatewayStore
	chainID               *big.Int
	onboarderAddress      common.Address
	batchOnboarderAddress common.Address
	unknown               gateway.UnknownGatewayLogger
}

func withApiContext(h func(w http.ResponseWriter, r *http.Request, ctx *apiContext), ctx *apiContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h(w, r, ctx)
	}
}

func AddGateway(w http.ResponseWriter, r *http.Request, ctx *apiContext) {
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

	gw, err := ctx.gateways.ByLocalID(req.LocalID)
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
		gw, err = ctx.gateways.Add(r.Context(), gw.LocalID, gw.PrivateKey)
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

func ImportGateways(w http.ResponseWriter, r *http.Request, ctx *apiContext) {
	var (
		req struct {
			Owner common.Address `json:"owner"`
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

	recg, err := ctx.unknown.Recorded()
	if err == nil {
		for _, rec := range recg {
			if !ctx.gateways.ContainsByLocalID(rec.LocalID) {
				gw, err := gateway.GenerateNewGateway(rec.LocalID)
				if err != nil {
					logrus.WithError(err).Error("unable to generate new gateway entry")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}

				gw, err = ctx.gateways.Add(r.Context(), gw.LocalID, gw.PrivateKey)
				if err != nil {
					logrus.WithError(err).Error("unable to add new gateway entry to store")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}

				signature, err := gateway.SignPlainBatchOnboardMessage(ctx.chainID, ctx.batchOnboarderAddress, req.Owner, 0, gw)
				if err != nil {
					logrus.WithError(err).Error("unable to sign onboard message")
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					return
				}

				statusCode = http.StatusCreated

				reply = append(reply, OnboardGatewayReply{
					Owner:                   req.Owner,
					Address:                 gw.Address(),
					ChainID:                 ctx.chainID.Uint64(),
					GatewayID:               gw.ID(),
					GatewayOnboardSignature: "0x" + hex.EncodeToString(signature),
					LocalID:                 gw.LocalID,
					NetworkID:               gw.NetworkID,
					Version:                 0,
					Onboarder:               ctx.batchOnboarderAddress,
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

func OnboardGatewayMessage(w http.ResponseWriter, r *http.Request, ctx *apiContext) {
	var (
		req struct {
			LocalID lorawan.EUI64  `json:"localId"`
			Owner   common.Address `json:"owner"`
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
	gw, err := ctx.gateways.ByLocalID(req.LocalID)
	if err != nil && errors.Is(err, gateway.ErrNotFound) {
		gw, err = gateway.GenerateNewGateway(req.LocalID)
		if err != nil {
			logrus.WithError(err).Error("unable to generate new gateway entry")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		gw, err = ctx.gateways.Add(r.Context(), gw.LocalID, gw.PrivateKey)
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

	signature, err := gateway.SignPlainBatchOnboardMessage(ctx.chainID, ctx.onboarderAddress, req.Owner, 0, gw)
	if err != nil {
		logrus.WithError(err).Error("unable to sign onboard message")
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	replyJSON(w, statusCode, &OnboardGatewayReply{
		Owner:                   req.Owner,
		Address:                 gw.Address(),
		ChainID:                 ctx.chainID.Uint64(),
		GatewayID:               gw.ID(),
		GatewayOnboardSignature: fmt.Sprintf("0x%x", signature),
		LocalID:                 gw.LocalID,
		NetworkID:               gw.NetworkID,
		Version:                 0,
		Onboarder:               ctx.onboarderAddress,
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

func ListGateways(w http.ResponseWriter, r *http.Request, ctx *apiContext) {
	var collector gateway.Collector
	ctx.gateways.Range(&collector)

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

func ListUnknownGateways(w http.ResponseWriter, r *http.Request, ctx *apiContext) {
	recg, err := ctx.unknown.Recorded()
	switch {
	case err == nil:
		filtered := make([]*gateway.RecordedUnknownGateway, 0)
		for _, r := range recg {
			if !ctx.gateways.ContainsByLocalID(r.LocalID) {
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

func Gateway(w http.ResponseWriter, r *http.Request, ctx *apiContext) {
	localID, err := utils.Eui64FromString(chi.URLParam(r, "local_id"))
	if err != nil {
		http.Error(w, "invalid gateway local id", http.StatusBadRequest)
		return
	}

	gw, err := ctx.gateways.ByLocalID(localID)
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

func SyncGateway(w http.ResponseWriter, r *http.Request, ctx *apiContext) {
	localID, err := utils.Eui64FromString(chi.URLParam(r, "local_id"))
	if err != nil {
		http.Error(w, "invalid gateway local id", http.StatusBadRequest)
		return
	}
	gw, err := ctx.gateways.SyncGatewayByLocalID(r.Context(), localID, true)
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

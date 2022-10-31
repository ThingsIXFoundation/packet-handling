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

package utils

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/mitchellh/mapstructure"
	"github.com/sirupsen/logrus"
)

// StringToEthAddressHook converts from a string to an ethereum address when required
func StringToEthereumAddressHook() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String || t != reflect.TypeOf(common.Address{}) {
			return data, nil
		}
		return common.HexToAddress(data.(string)), nil
	}
}

// HexStringToBigIntHook converts from a string to a big.Int when required
func HexStringToBigIntHook() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String || t != reflect.TypeOf(new(big.Int)) {
			return data, nil
		}
		biStr := data.(string)
		if strings.HasPrefix(biStr, "0x") || strings.HasPrefix(biStr, "0X") {
			biStr = biStr[2:]
		}
		bi, ok := new(big.Int).SetString(biStr, 16)
		if ok {
			return bi, nil
		}
		return nil, fmt.Errorf("could not parse hex string to big.Int")
	}
}

// IntToBigIntHook converts a int to a *big.Int when required
func IntToBigIntHook() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.Int || t != reflect.TypeOf(new(big.Int)) {
			return data, nil
		}
		return big.NewInt(int64(data.(int))), nil
	}
}

// StringToHashHook converts from a string to a ecommon.Hash when required
func StringToHashHook() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String || t != reflect.TypeOf(common.Hash{}) {
			return data, nil
		}
		return common.HexToHash(data.(string)), nil
	}
}

var duration time.Duration

// StringToDuration converts from a string into a time.Duration when required
func StringToDuration() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String || t != reflect.TypeOf(duration) {
			return data, nil
		}
		return time.ParseDuration(data.(string))
	}
}

var lvl logrus.Level

// StringToLogrusLevel converts from a string into a logrus.Level when required
func StringToLogrusLevel() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
		if f.Kind() != reflect.String || t != reflect.TypeOf(lvl) {
			return data, nil
		}
		switch strings.ToLower(data.(string)) {
		case "trace":
			return logrus.TraceLevel, nil
		case "debug":
			return logrus.DebugLevel, nil
		case "info":
			return logrus.InfoLevel, nil
		case "warn":
			return logrus.WarnLevel, nil
		case "error":
			return logrus.ErrorLevel, nil
		case "fatal":
			return logrus.FatalLevel, nil
		case "panic":
			return logrus.PanicLevel, nil
		default:
			return nil, fmt.Errorf("invalid log level %s", data)
		}
	}
}

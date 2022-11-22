# Copyright 2022 Stichting ThingsIX Foundation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# SPDX-License-Identifier: Apache-2.0

FROM --platform=$BUILDPLATFORM golang:1.19.3-alpine as build

RUN apk add --update-cache ca-certificates && rm -rf /var/cache/apk/*

WORKDIR /build

COPY go.mod .
COPY go.sum .

RUN go mod download
RUN go mod verify

COPY . .
RUN cd cmd/forwarder && CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /forwarder

# copy forwarder and certs to base image.
FROM scratch 

LABEL authors="ThingsIX Foundation"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /forwarder .

ENTRYPOINT ["./forwarder"]

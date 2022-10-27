FROM golang:1.19.2-alpine as build

ARG GITHUB_ACCESS_TOKEN

RUN apk add --update-cache build-base musl-dev git ca-certificates && rm -rf /var/cache/apk/*

ENV GOPRIVATE=github.com/ThingsIXFoundation

RUN git config --global url."https://${GITHUB_ACCESS_TOKEN}:x-oauth-basic@github.com".insteadOf "https://github.com"

WORKDIR /build

COPY go.mod .
COPY go.sum .

RUN go mod download
RUN go mod verify

COPY . .
RUN cd cmd/forwarder && go build -ldflags="-s -w -extldflags '-static'" -o /forwarder

# Now copy it into our base image.
FROM scratch

LABEL authors="ThingsIX Foundation"

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /forwarder .

ENTRYPOINT ["./forwarder"]
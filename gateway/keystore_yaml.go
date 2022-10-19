package gateway

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/crypto"
	"gopkg.in/yaml.v3"
)

type GatewayYamlFileStore struct {
	Path     string
	gateways []*Gateway
}

func LoadGatewayYamlFileStore(path string) (*GatewayYamlFileStore, error) {
	store := &GatewayYamlFileStore{
		Path: path,
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

type gatewayYAML struct {
	LocalID    lorawan.EUI64 `yaml:"local_id"`
	PrivateKey string        `yaml:"private_key"`
}

func (store *GatewayYamlFileStore) Gateways() ([]*Gateway, error) {
	return store.gateways, nil
}

func (store *GatewayYamlFileStore) GatewayByThingsIxID(id [32]byte) (*Gateway, error) {
	for _, gw := range store.gateways {
		if bytes.Equal(gw.CompressedPublicKeyBytes, id[:]) {
			return gw, nil
		}
	}
	return nil, ErrNotFound
}

func (store *GatewayYamlFileStore) GatewayByLocalID(id lorawan.EUI64) (*Gateway, error) {
	for _, gw := range store.gateways {
		if gw.LocalGatewayID == id {
			return gw, nil
		}
	}
	return nil, ErrNotFound
}

func (store *GatewayYamlFileStore) GatewayByNetworkID(id lorawan.EUI64) (*Gateway, error) {
	for _, gw := range store.gateways {
		if gw.NetworkGatewayID == id {
			return gw, nil
		}
	}
	return nil, ErrNotFound
}

func (store *GatewayYamlFileStore) AddGateway(localID lorawan.EUI64, key *ecdsa.PrivateKey) error {
	if _, err := store.GatewayByLocalID(localID); err != ErrNotFound {
		if err == nil {
			return ErrAlreadyExists
		}
		return err
	}

	// serialize gateway details
	encoded, err := yaml.Marshal([]gatewayYAML{{
		LocalID:    localID,
		PrivateKey: hex.EncodeToString(crypto.FromECDSA(key)),
	}})
	if err != nil {
		return fmt.Errorf("unable to encode gateway store: %w", err)
	}

	// append gateway to store
	f, err := os.OpenFile(store.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(encoded); err != nil {
		return err
	}
	return store.load()
}

func (store *GatewayYamlFileStore) load() error {
	gatewaysFile, err := os.Open(store.Path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("unable to open gateway store: %w", err)
		}
		return nil
	}
	defer gatewaysFile.Close()

	var gateways []gatewayYAML
	if err := yaml.NewDecoder(gatewaysFile).Decode(&gateways); err != nil {
		return fmt.Errorf("unable to load gateway store: %w", err)
	}

	loaded := make([]*Gateway, len(gateways))
	for i, gw := range gateways {
		key, err := crypto.HexToECDSA(gw.PrivateKey)
		if err != nil {
			return fmt.Errorf("could not decode private key from gateway store")
		}
		if loaded[i], err = NewGateway(gw.LocalID, key); err != nil {
			return fmt.Errorf("unable to load gateway from store: %w", err)
		}
	}

	store.gateways = loaded

	return nil
}

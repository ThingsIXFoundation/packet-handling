package file

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ethereum/go-ethereum/crypto"
)

type FileGatewayStore struct {
	path     string
	gateways []*gateway.Gateway
}

var _ gateway.GatewayStore = (*FileGatewayStore)(nil)

func NewFileGatewayStore(path string) (*FileGatewayStore, error) {
	fs := &FileGatewayStore{path: path}
	err := fs.readKeys()
	if err != nil {
		return nil, err
	}
	return fs, nil
}

func (ks *FileGatewayStore) Gateways() ([]*gateway.Gateway, error) {
	return ks.gateways, nil
}

func (ks *FileGatewayStore) GatewayByLocalID(localGatewayID []byte) (*gateway.Gateway, error) {
	for _, gateway := range ks.gateways {
		if bytes.Equal(localGatewayID, gateway.LocalGatewayID.Bytes()) {
			return gateway, nil
		}
	}

	return nil, nil
}

func (ks *FileGatewayStore) GatewayByNetworkID(networkGatewayID []byte) (*gateway.Gateway, error) {
	for _, gateway := range ks.gateways {
		if bytes.Equal(networkGatewayID, gateway.NetworkGatewayID.Bytes()) {
			return gateway, nil
		}
	}

	return nil, nil
}

func (ks *FileGatewayStore) AddGateway(gw *gateway.Gateway) error {
	egw, err := ks.GatewayByLocalID(gw.LocalGatewayID[:])
	if err != nil {
		return err
	} else if egw != nil {
		return fmt.Errorf("gateway: %s, already exists in keystore", gw.LocalGatewayID)
	}

	f, err := os.OpenFile(ks.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	f.WriteString(fmt.Sprintf("%s:%s\n", gw.LocalGatewayID, hex.EncodeToString(crypto.FromECDSA(gw.PrivateKey))))
	ks.readKeys()

	return nil

}

func (ks *FileGatewayStore) readKeys() error {
	f, err := os.OpenFile(ks.path, os.O_RDONLY, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	var gateways []*gateway.Gateway

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		split := strings.Split(line, ":")
		if len(split) != 2 {
			return fmt.Errorf("invalid file format")
		}

		gatewayIDs := split[0]
		gatewayID, err := hex.DecodeString(gatewayIDs)
		if err != nil {
			return err
		}
		priv, err := crypto.HexToECDSA(split[1])
		if err != nil {
			return err
		}

		gateway, err := gateway.NewGateway(gatewayID, priv)
		if err != nil {
			return err
		}

		gateways = append(gateways, gateway)
	}

	ks.gateways = gateways

	return nil
}

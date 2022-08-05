package file

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/ThingsIXFoundation/packet-handling/gateway"
	"github.com/ethereum/go-ethereum/crypto"
)

type FileGatewayStore struct {
	path        string
	privateKeys map[string]*ecdsa.PrivateKey
	networkIds  map[string][]byte
}

var _ gateway.GatewayStore = (*FileGatewayStore)(nil)

func NewFileKeyStore(path string) (*FileGatewayStore, error) {
	return &FileGatewayStore{path: path}, nil
}

func (ks *FileGatewayStore) Gateways() ([][]byte, error) {
	var ids []byte
	for gatewayIdHex, _ := range ks.privateKeys {

	}
}

func (ks *FileGatewayStore) CreateKeyPairForGateway(gatewayId []byte) error {
	// Read the file again so we have the latest version
	ks.readKeys()
	if _, ok := ks.privateKeys[hex.EncodeToString(gatewayId)]; ok {
		return fmt.Errorf("gateway: %d, already exists in keystore", hex.EncodeToString(gatewayId))
	}

	f, err := os.OpenFile(ks.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	priv, err := crypto.GenerateKey()

	f.WriteString(fmt.Sprintf("%s:%s", hex.EncodeToString(gatewayId), crypto.FromECDSA(priv)))

	return nil
}

func (ks *FileGatewayStore) PrivateKeyForGateway(gatewayId []byte) *ecdsa.PrivateKey {
	return ks.privateKeys[hex.EncodeToString(gatewayId)]
}

func (ks *FileGatewayStore) PublicKeyForGateway(gatewayId []byte) *ecdsa.PublicKey {
	priv := ks.privateKeys[hex.EncodeToString(gatewayId)]
	if priv == nil {
		return nil
	}

	pub := priv.PublicKey

	return &pub

}

func (ks *FileGatewayStore) NetworkGatewayIdForGateway(gatewayId []byte) []byte {
	return ks.networkIds[hex.EncodeToString(gatewayId)]
}

func (ks *FileGatewayStore) GatewayIdForNetworkGatewayId(networkGatewayId []byte) []byte {
	for gids, ngid := range ks.networkIds {
		if bytes.Equal(ngid, networkGatewayId) {
			gid, err := hex.DecodeString(gids)
			if err != nil {
				return nil
			}
			return gid
		}
	}

	return nil
}

func (ks *FileGatewayStore) readKeys() error {
	f, err := os.OpenFile(ks.path, os.O_RDONLY, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		split := strings.Split(line, ":")
		if len(split) != 2 {
			return fmt.Errorf("invalid file format")
		}

		gatewayId := split[0]
		key, err := crypto.HexToECDSA(split[1])
		if err != nil {
			return err
		}

		ks.privateKeys[gatewayId] = key
		ks.networkIds[gatewayId] = calculateNetworkId(key)
	}

	return nil
}

func calculateNetworkId(priv *ecdsa.PrivateKey) []byte {
	pub := priv.PublicKey
	pubBytes := crypto.FromECDSAPub(&pub)
	h := sha256.Sum256(pubBytes)
	return h[0:8]
}

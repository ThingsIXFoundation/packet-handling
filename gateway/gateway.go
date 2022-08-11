package gateway

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/crypto"
)

type Gateway struct {
	LocalGatewayID   GatewayID
	NetworkGatewayID GatewayID
	PrivateKey       *ecdsa.PrivateKey
	PublicKey        *ecdsa.PublicKey
	PublicKeyBytes   []byte
	Owner            string
}

func NewGateway(localGatewayIDBytes []byte, priv *ecdsa.PrivateKey) (*Gateway, error) {
	localGatewayID, err := NewGatewayID(localGatewayIDBytes)
	if err != nil {
		return nil, err
	}
	return &Gateway{
		LocalGatewayID:   localGatewayID,
		NetworkGatewayID: CalculateNetworkGatewayID(priv),
		PrivateKey:       priv,
		PublicKey:        &priv.PublicKey,
		PublicKeyBytes:   CalculatePublicKeyBytes(&priv.PublicKey),
		Owner:            "", // TODO
	}, nil
}

func GenerateNewGateway(localGatewayIDBytes []byte) (*Gateway, error) {
	localGatewayID, err := NewGatewayID(localGatewayIDBytes)
	if err != nil {
		return nil, err
	}

	priv, err := GeneratePrivateKey()
	if err != nil {
		return nil, err
	}

	return &Gateway{
		LocalGatewayID:   localGatewayID,
		NetworkGatewayID: CalculateNetworkGatewayID(priv),
		PrivateKey:       priv,
		PublicKey:        &priv.PublicKey,
		PublicKeyBytes:   CalculatePublicKeyBytes(&priv.PublicKey),
		Owner:            "", // TODO
	}, nil
}

func CalculateNetworkGatewayID(priv *ecdsa.PrivateKey) GatewayID {
	pub := priv.PublicKey
	pubBytes := CalculatePublicKeyBytes(&pub)
	h := sha256.Sum256(pubBytes)

	gatewayID, _ := NewGatewayID(h[0:8])

	return gatewayID
}

func CalculatePublicKeyBytes(pub *ecdsa.PublicKey) []byte {
	return crypto.CompressPubkey(pub)[1:]
}

func (gw *Gateway) Address() string {
	return crypto.PubkeyToAddress(*gw.PublicKey).String()
}

type GatewayID lorawan.EUI64

func NewGatewayID(gatewayIDbytes []byte) (GatewayID, error) {
	if len(gatewayIDbytes) != 8 {
		return GatewayID{}, fmt.Errorf("invalid gateway-id length: %d", len(gatewayIDbytes))
	}

	var gatewayID GatewayID
	copy(gatewayID[:], gatewayIDbytes)

	return gatewayID, nil
}

func NewGatewayIDFromString(gatewayIDStr string) (GatewayID, error) {
	gatewayIDStr = strings.ReplaceAll(gatewayIDStr, ":", "")
	gatewayIDStr = strings.ReplaceAll(gatewayIDStr, " ", "")
	gatewayIDbytes, err := hex.DecodeString(gatewayIDStr)
	if err != nil {
		return GatewayID{}, err
	}

	return NewGatewayID(gatewayIDbytes)

}

func (gid GatewayID) String() string {
	return hex.EncodeToString(gid[:])
}

func (gid GatewayID) Bytes() []byte {
	return gid[:]
}

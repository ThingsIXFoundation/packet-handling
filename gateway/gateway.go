package gateway

import (
	"crypto/ecdsa"
	"crypto/sha256"

	"github.com/ThingsIXFoundation/packet-handling/utils"
	"github.com/brocaar/lorawan"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type Gateway struct {
	LocalGatewayID           lorawan.EUI64
	NetworkGatewayID         lorawan.EUI64
	PrivateKey               *ecdsa.PrivateKey
	PublicKey                *ecdsa.PublicKey
	PublicKeyBytes           []byte
	CompressedPublicKeyBytes []byte
	Owner                    common.Address
}

func NewGateway(localGatewayID lorawan.EUI64, priv *ecdsa.PrivateKey) (*Gateway, error) {
	return &Gateway{
		LocalGatewayID:           localGatewayID,
		NetworkGatewayID:         CalculateNetworkGatewayID(priv),
		PrivateKey:               priv,
		PublicKey:                &priv.PublicKey,
		CompressedPublicKeyBytes: CalculateCompressedPublicKeyBytes(&priv.PublicKey),
		PublicKeyBytes:           CalculatePublicKeyBytes(&priv.PublicKey),
		Owner:                    common.Address{}, // TODO
	}, nil
}

func GenerateNewGateway(localID lorawan.EUI64) (*Gateway, error) {
	priv, err := GeneratePrivateKey()
	if err != nil {
		return nil, err
	}

	return &Gateway{
		LocalGatewayID:           localID,
		NetworkGatewayID:         CalculateNetworkGatewayID(priv),
		PrivateKey:               priv,
		PublicKey:                &priv.PublicKey,
		CompressedPublicKeyBytes: CalculateCompressedPublicKeyBytes(&priv.PublicKey),
		Owner:                    common.Address{}, // TODO
	}, nil
}

func CalculateNetworkGatewayID(priv *ecdsa.PrivateKey) lorawan.EUI64 {
	pub := priv.PublicKey
	pubBytes := CalculateCompressedPublicKeyBytes(&pub)
	h := sha256.Sum256(pubBytes)

	gatewayID, _ := utils.BytesToGatewayID(h[0:8])

	return gatewayID
}

func CalculateCompressedPublicKeyBytes(pub *ecdsa.PublicKey) []byte {
	return crypto.CompressPubkey(pub)[1:]
}

func CalculatePublicKeyBytes(pub *ecdsa.PublicKey) []byte {
	return crypto.FromECDSAPub(pub)
}

func (gw *Gateway) Address() string {
	return crypto.PubkeyToAddress(*gw.PublicKey).String()
}

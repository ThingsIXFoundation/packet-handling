package gateway

import (
	"crypto/ecdsa"

	"github.com/ethereum/go-ethereum/crypto"
)

func GeneratePrivateKey() (*ecdsa.PrivateKey, error) {
	for {
		priv, err := crypto.GenerateKey()
		if err != nil {
			return nil, err
		}

		compressedPub := crypto.CompressPubkey(&priv.PublicKey)
		if compressedPub[0] == 0x02 {
			return priv, nil
		}
	}
}

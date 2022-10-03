package utils

import (
	"crypto/sha256"
	"fmt"
)

func GatewayPublicKeyToID(pubKey []byte) ([]byte, error) {
	// pubkey is the compressed 33-byte long public key,
	// the gateway ID is the pub key without the 0x02 prefix
	if len(pubKey) == 33 {
		// compressed ThingsIX public keys always start with 0x02.
		// therefore don't use it and use bytes [1:] to derive the id
		h := sha256.Sum256(pubKey[1:])
		return h[:8], nil
	}
	return nil, fmt.Errorf("invalid gateway public key (len=%d)", len(pubKey))
}

package utils

import (
	"crypto/sha256"
	"fmt"

	"github.com/brocaar/lorawan"
)

func GatewayPublicKeyToID(pubKey []byte) (lorawan.EUI64, error) {
	// pubkey is the compressed 33-byte long public key,
	// the gateway ID is the pub key without the 0x02 prefix
	if len(pubKey) == 33 {
		// compressed ThingsIX public keys always start with 0x02.
		// therefore don't use it and use bytes [1:] to derive the id
		h := sha256.Sum256(pubKey[1:])
		return BytesToGatewayID(h[:8])
	}
	return lorawan.EUI64{}, fmt.Errorf("invalid gateway public key (len=%d)", len(pubKey))
}

func BytesToGatewayID(id []byte) (lorawan.EUI64, error) {
	var gid lorawan.EUI64
	if len(id) != len(gid) {
		return lorawan.EUI64{}, fmt.Errorf("invalid gateway id length (len=%d)", len(id))
	}
	copy(gid[:], id)
	return gid, nil
}

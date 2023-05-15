package utils

import (
	"encoding/hex"

	"github.com/brocaar/lorawan"
)

// NetIDType returns the NetID type of the DevAddr.
func NetIDType(a lorawan.DevAddr) int {
	for i := 7; i >= 0; i-- {
		if a[0]&(1<<byte(i)) == 0 {
			return 7 - i
		}
	}

	return -1
}

func NwkId(a lorawan.DevAddr) []byte {
	if NetIDType(a) < 0 {
		return nil
	} else {
		return a.NwkID()
	}
}

func NwkIdString(a lorawan.DevAddr) string {
	nwkid := NwkId(a)
	if nwkid == nil {
		return "invalid"
	} else {
		return hex.EncodeToString(nwkid)
	}
}

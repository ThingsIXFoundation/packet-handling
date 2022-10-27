package gateway

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// SignPlainBatchOnboardMessage signs a gateway onboard message for the PlainBatchGatewayOnboarder smart contract
func SignPlainBatchOnboardMessage(chainID *big.Int, contract common.Address, owner common.Address, version uint8, gw *Gateway) ([]byte, error) {
	var (
		onbw, _        = abi.NewType("string", "", nil)
		sep, _         = abi.NewType("string", "", nil)
		chainIDT, _    = abi.NewType("uint256", "", nil)
		onbAddr, _     = abi.NewType("address", "", nil)
		versionT, _    = abi.NewType("uint8", "", nil)
		gatewayID, _   = abi.NewType("bytes32", "", nil)
		gatewayAddr, _ = abi.NewType("address", "", nil)
		ownerT, _      = abi.NewType("address", "", nil)
		args           = abi.Arguments{
			{Type: onbw},
			{Type: sep},
			{Type: chainIDT},
			{Type: sep},
			{Type: onbAddr},
			{Type: sep},
			{Type: versionT},
			{Type: sep},
			{Type: gatewayID},
			{Type: sep},
			{Type: gatewayAddr},
			{Type: sep},
			{Type: ownerT},
		}
	)

	packed, err := args.Pack("ONBGW", "|", chainID, "|", contract, "|", version, "|", gw.ID(), "|", gw.Address(), "|", owner)
	if err != nil {
		return nil, fmt.Errorf("unable to pack onboard message: %w", err)
	}

	h := crypto.Keccak256Hash(packed)
	sign, err := crypto.Sign(h[:], gw.PrivateKey)
	if err != nil {
		return nil, err
	}
	sign[64] += 27 // solidity wants [27,28]
	return sign, nil
}

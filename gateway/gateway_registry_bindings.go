// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package gateway

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// Struct0 is an auto generated low-level Go binding around an user-defined struct.
type Struct0 struct {
	PublicKey     [32]byte
	Version       uint8
	Owner         common.Address
	AntennaGain   uint8
	FrequencyPlan uint8
	Location      int64
	Altitude      uint8
}

// GatewayRegistryMetaData contains all meta data concerning the GatewayRegistry contract.
var GatewayRegistryMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"id\",\"type\":\"bytes32\"}],\"name\":\"gateways\",\"outputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"publicKey\",\"type\":\"bytes32\"},{\"internalType\":\"uint8\",\"name\":\"version\",\"type\":\"uint8\"},{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"uint8\",\"name\":\"antennaGain\",\"type\":\"uint8\"},{\"internalType\":\"uint8\",\"name\":\"frequencyPlan\",\"type\":\"uint8\"},{\"internalType\":\"int64\",\"name\":\"location\",\"type\":\"int64\"},{\"internalType\":\"uint8\",\"name\":\"altitude\",\"type\":\"uint8\"}],\"internalType\":\"structIGatewayRegistry.Gateway\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// GatewayRegistryABI is the input ABI used to generate the binding from.
// Deprecated: Use GatewayRegistryMetaData.ABI instead.
var GatewayRegistryABI = GatewayRegistryMetaData.ABI

// GatewayRegistry is an auto generated Go binding around an Ethereum contract.
type GatewayRegistry struct {
	GatewayRegistryCaller     // Read-only binding to the contract
	GatewayRegistryTransactor // Write-only binding to the contract
	GatewayRegistryFilterer   // Log filterer for contract events
}

// GatewayRegistryCaller is an auto generated read-only Go binding around an Ethereum contract.
type GatewayRegistryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GatewayRegistryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type GatewayRegistryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GatewayRegistryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type GatewayRegistryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// GatewayRegistrySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type GatewayRegistrySession struct {
	Contract     *GatewayRegistry  // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// GatewayRegistryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type GatewayRegistryCallerSession struct {
	Contract *GatewayRegistryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts          // Call options to use throughout this session
}

// GatewayRegistryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type GatewayRegistryTransactorSession struct {
	Contract     *GatewayRegistryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts          // Transaction auth options to use throughout this session
}

// GatewayRegistryRaw is an auto generated low-level Go binding around an Ethereum contract.
type GatewayRegistryRaw struct {
	Contract *GatewayRegistry // Generic contract binding to access the raw methods on
}

// GatewayRegistryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type GatewayRegistryCallerRaw struct {
	Contract *GatewayRegistryCaller // Generic read-only contract binding to access the raw methods on
}

// GatewayRegistryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type GatewayRegistryTransactorRaw struct {
	Contract *GatewayRegistryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewGatewayRegistry creates a new instance of GatewayRegistry, bound to a specific deployed contract.
func NewGatewayRegistry(address common.Address, backend bind.ContractBackend) (*GatewayRegistry, error) {
	contract, err := bindGatewayRegistry(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &GatewayRegistry{GatewayRegistryCaller: GatewayRegistryCaller{contract: contract}, GatewayRegistryTransactor: GatewayRegistryTransactor{contract: contract}, GatewayRegistryFilterer: GatewayRegistryFilterer{contract: contract}}, nil
}

// NewGatewayRegistryCaller creates a new read-only instance of GatewayRegistry, bound to a specific deployed contract.
func NewGatewayRegistryCaller(address common.Address, caller bind.ContractCaller) (*GatewayRegistryCaller, error) {
	contract, err := bindGatewayRegistry(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &GatewayRegistryCaller{contract: contract}, nil
}

// NewGatewayRegistryTransactor creates a new write-only instance of GatewayRegistry, bound to a specific deployed contract.
func NewGatewayRegistryTransactor(address common.Address, transactor bind.ContractTransactor) (*GatewayRegistryTransactor, error) {
	contract, err := bindGatewayRegistry(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &GatewayRegistryTransactor{contract: contract}, nil
}

// NewGatewayRegistryFilterer creates a new log filterer instance of GatewayRegistry, bound to a specific deployed contract.
func NewGatewayRegistryFilterer(address common.Address, filterer bind.ContractFilterer) (*GatewayRegistryFilterer, error) {
	contract, err := bindGatewayRegistry(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &GatewayRegistryFilterer{contract: contract}, nil
}

// bindGatewayRegistry binds a generic wrapper to an already deployed contract.
func bindGatewayRegistry(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := GatewayRegistryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_GatewayRegistry *GatewayRegistryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _GatewayRegistry.Contract.GatewayRegistryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_GatewayRegistry *GatewayRegistryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _GatewayRegistry.Contract.GatewayRegistryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_GatewayRegistry *GatewayRegistryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _GatewayRegistry.Contract.GatewayRegistryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_GatewayRegistry *GatewayRegistryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _GatewayRegistry.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_GatewayRegistry *GatewayRegistryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _GatewayRegistry.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_GatewayRegistry *GatewayRegistryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _GatewayRegistry.Contract.contract.Transact(opts, method, params...)
}

// Gateways is a free data retrieval call binding the contract method 0xfbe336ff.
//
// Solidity: function gateways(bytes32 id) view returns((bytes32,uint8,address,uint8,uint8,int64,uint8))
func (_GatewayRegistry *GatewayRegistryCaller) Gateways(opts *bind.CallOpts, id [32]byte) (Struct0, error) {
	var out []interface{}
	err := _GatewayRegistry.contract.Call(opts, &out, "gateways", id)

	if err != nil {
		return *new(Struct0), err
	}

	out0 := *abi.ConvertType(out[0], new(Struct0)).(*Struct0)

	return out0, err

}

// Gateways is a free data retrieval call binding the contract method 0xfbe336ff.
//
// Solidity: function gateways(bytes32 id) view returns((bytes32,uint8,address,uint8,uint8,int64,uint8))
func (_GatewayRegistry *GatewayRegistrySession) Gateways(id [32]byte) (Struct0, error) {
	return _GatewayRegistry.Contract.Gateways(&_GatewayRegistry.CallOpts, id)
}

// Gateways is a free data retrieval call binding the contract method 0xfbe336ff.
//
// Solidity: function gateways(bytes32 id) view returns((bytes32,uint8,address,uint8,uint8,int64,uint8))
func (_GatewayRegistry *GatewayRegistryCallerSession) Gateways(id [32]byte) (Struct0, error) {
	return _GatewayRegistry.Contract.Gateways(&_GatewayRegistry.CallOpts, id)
}


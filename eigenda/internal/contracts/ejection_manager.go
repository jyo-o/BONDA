package contracts

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// OperatorEjected event: OperatorEjected(bytes32 indexed operatorId, uint8 quorumNumber)
var OperatorEjectedTopic = crypto.Keccak256Hash([]byte("OperatorEjected(bytes32,uint8)"))

// DefaultEjectionManagerAddress is a placeholder; the actual address is resolved at runtime
// from the EigenDADirectory contract.
var DefaultEjectionManagerAddress = common.HexToAddress("0x0000000000000000000000000000000000000000")

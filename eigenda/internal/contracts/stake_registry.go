package contracts

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const stakeRegistryABI = `[
	{
		"inputs": [
			{"internalType": "bytes32", "name": "operatorId", "type": "bytes32"},
			{"internalType": "uint8", "name": "quorumNumber", "type": "uint8"}
		],
		"name": "getCurrentStake",
		"outputs": [{"internalType": "uint96", "name": "", "type": "uint96"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

type StakeRegistryClient struct {
	client *ethclient.Client
	addr   common.Address
	abi    abi.ABI
}

func NewStakeRegistryClient(client *ethclient.Client, addr common.Address) (*StakeRegistryClient, error) {
	parsed, err := abi.JSON(strings.NewReader(stakeRegistryABI))
	if err != nil {
		return nil, err
	}
	return &StakeRegistryClient{client: client, addr: addr, abi: parsed}, nil
}

func (s *StakeRegistryClient) GetCurrentStake(ctx context.Context, operatorID [32]byte, quorumNumber uint8) (*big.Int, error) {
	data, err := s.abi.Pack("getCurrentStake", operatorID, quorumNumber)
	if err != nil {
		return nil, fmt.Errorf("pack getCurrentStake: %w", err)
	}
	result, err := s.client.CallContract(ctx, ethereum.CallMsg{To: &s.addr, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("call getCurrentStake: %w", err)
	}
	values, err := s.abi.Unpack("getCurrentStake", result)
	if err != nil || len(values) == 0 {
		return nil, fmt.Errorf("unpack getCurrentStake: %w", err)
	}
	stake, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected type for stake")
	}
	return stake, nil
}

package contracts

import (
	"context"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const directoryABIJSON = `[
	{
		"inputs": [{"internalType": "string", "name": "name", "type": "string"}],
		"name": "getAddress",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

func ResolveAddress(ctx context.Context, client *ethclient.Client, directoryAddr common.Address, name string) (common.Address, error) {
	parsed, err := abi.JSON(strings.NewReader(directoryABIJSON))
	if err != nil {
		return common.Address{}, err
	}
	data, err := parsed.Pack("getAddress", name)
	if err != nil {
		return common.Address{}, err
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &directoryAddr, Data: data}, nil)
	if err != nil {
		return common.Address{}, err
	}
	values, err := parsed.Unpack("getAddress", result)
	if err != nil || len(values) == 0 {
		return common.Address{}, fmt.Errorf("unpack getAddress(%s) failed", name)
	}
	addr, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected type for address")
	}
	return addr, nil
}

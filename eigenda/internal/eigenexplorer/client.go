// Package eigenexplorer resolves EigenDA operator metadata directly from
// on-chain contracts (RegistryCoordinator + DelegationManager events),
// requiring only an existing ETH RPC — no external API key needed.
package eigenexplorer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Mainnet contract addresses
var (
	DelegationManagerAddr = common.HexToAddress("0x39053D51B77DC0d36036Fc1fCc8Cb819df8EF37A")
)

// OperatorMetadataURIUpdated event signature
var metadataURIEventSig = crypto.Keccak256Hash([]byte("OperatorMetadataURIUpdated(address,string)"))

// ABI for RegistryCoordinator.getOperatorFromId(bytes32) → address
const registryCoordinatorABI = `[
	{
		"inputs": [{"internalType": "bytes32", "name": "operatorId", "type": "bytes32"}],
		"name": "getOperatorFromId",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// ABI for decoding OperatorMetadataURIUpdated event non-indexed data
const delegationManagerEventABI = `[
	{
		"anonymous": false,
		"inputs": [
			{"indexed": true, "internalType": "address", "name": "operator", "type": "address"},
			{"indexed": false, "internalType": "string", "name": "metadataURI", "type": "string"}
		],
		"name": "OperatorMetadataURIUpdated",
		"type": "event"
	}
]`

// OperatorMeta holds resolved metadata for an operator.
type OperatorMeta struct {
	Address     common.Address
	Name        string `json:"name"`
	Description string `json:"description"`
	Logo        string `json:"logo"`
	Website     string `json:"website"`
	Twitter     string `json:"twitter"`
}

// Client resolves operator metadata from on-chain data.
type Client struct {
	ethClient    *ethclient.Client
	regCoordAddr common.Address
	regCoordABI  abi.ABI
	eventABI     abi.ABI
	httpClient   *http.Client
}

// NewClient creates a metadata resolver.
// regCoordAddr is the EigenDA RegistryCoordinator address
// (resolvable from the EigenDA Directory via "REGISTRY_COORDINATOR").
func NewClient(ethClient *ethclient.Client, regCoordAddr common.Address) (*Client, error) {
	regABI, err := abi.JSON(strings.NewReader(registryCoordinatorABI))
	if err != nil {
		return nil, fmt.Errorf("parse registry coordinator ABI: %w", err)
	}
	evtABI, err := abi.JSON(strings.NewReader(delegationManagerEventABI))
	if err != nil {
		return nil, fmt.Errorf("parse delegation manager event ABI: %w", err)
	}
	return &Client{
		ethClient:    ethClient,
		regCoordAddr: regCoordAddr,
		regCoordABI:  regABI,
		eventABI:     evtABI,
		httpClient:   &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// ResolveOperatorAddress maps an EigenDA operator ID (BLS pubkey hash)
// to an Ethereum address via RegistryCoordinator.getOperatorFromId.
func (c *Client) ResolveOperatorAddress(ctx context.Context, operatorID [32]byte) (common.Address, error) {
	data, err := c.regCoordABI.Pack("getOperatorFromId", operatorID)
	if err != nil {
		return common.Address{}, err
	}
	result, err := c.ethClient.CallContract(ctx, ethereum.CallMsg{
		To:   &c.regCoordAddr,
		Data: data,
	}, nil)
	if err != nil {
		return common.Address{}, fmt.Errorf("call getOperatorFromId: %w", err)
	}
	values, err := c.regCoordABI.Unpack("getOperatorFromId", result)
	if err != nil || len(values) == 0 {
		return common.Address{}, fmt.Errorf("unpack getOperatorFromId failed")
	}
	addr, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected return type")
	}
	return addr, nil
}

// FetchMetadataURI finds the latest OperatorMetadataURIUpdated event
// for the given operator address from the DelegationManager.
func (c *Client) FetchMetadataURI(ctx context.Context, operatorAddr common.Address) (string, error) {
	// Search recent blocks (last ~90 days ≈ 650k blocks) for the event
	latestBlock, err := c.ethClient.BlockNumber(ctx)
	if err != nil {
		return "", fmt.Errorf("get latest block: %w", err)
	}

	// Alchemy free tier limits eth_getLogs to 10 blocks per call.
	// Instead of scanning, try a wide range first; if it fails,
	// fall back to progressively smaller ranges.
	operatorTopic := common.BytesToHash(operatorAddr.Bytes())

	ranges := [][2]uint64{
		{latestBlock - 650000, latestBlock},
		{latestBlock - 100000, latestBlock},
		{latestBlock - 10000, latestBlock},
		{latestBlock - 1000, latestBlock},
	}

	for _, r := range ranges {
		fromBlock := r[0]
		toBlock := r[1]

		query := ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(fromBlock),
			ToBlock:   new(big.Int).SetUint64(toBlock),
			Addresses: []common.Address{DelegationManagerAddr},
			Topics:    [][]common.Hash{{metadataURIEventSig}, {operatorTopic}},
		}

		logs, err := c.ethClient.FilterLogs(ctx, query)
		if err != nil {
			continue // try smaller range
		}

		if len(logs) == 0 {
			continue
		}

		// Take the latest event
		lastLog := logs[len(logs)-1]
		evt := c.eventABI.Events["OperatorMetadataURIUpdated"]
		values, err := evt.Inputs.UnpackValues(lastLog.Data)
		if err != nil || len(values) == 0 {
			return "", fmt.Errorf("unpack event data: %w", err)
		}
		uri, ok := values[0].(string)
		if !ok {
			return "", fmt.Errorf("unexpected URI type")
		}
		return uri, nil
	}

	return "", fmt.Errorf("no OperatorMetadataURIUpdated event found for %s", operatorAddr.Hex())
}

// FetchMetadata resolves the full metadata for an operator:
// operatorID → address → metadataURI → HTTP fetch → parsed JSON.
func (c *Client) FetchMetadata(ctx context.Context, operatorID [32]byte) (*OperatorMeta, error) {
	addr, err := c.ResolveOperatorAddress(ctx, operatorID)
	if err != nil {
		return nil, fmt.Errorf("resolve address: %w", err)
	}

	uri, err := c.FetchMetadataURI(ctx, addr)
	if err != nil {
		// Return partial result with address only
		return &OperatorMeta{Address: addr}, nil
	}

	meta, err := c.fetchMetadataJSON(uri)
	if err != nil {
		return &OperatorMeta{Address: addr}, nil
	}
	meta.Address = addr
	return meta, nil
}

func (c *Client) fetchMetadataJSON(uri string) (*OperatorMeta, error) {
	// Handle IPFS URIs
	if strings.HasPrefix(uri, "ipfs://") {
		uri = "https://ipfs.io/ipfs/" + strings.TrimPrefix(uri, "ipfs://")
	}

	resp, err := c.httpClient.Get(uri)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata URI: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("metadata URI returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var meta OperatorMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, fmt.Errorf("decode metadata JSON: %w", err)
	}
	return &meta, nil
}

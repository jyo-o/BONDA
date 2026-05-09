package registry

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// EigenDADirectory uses getAddress(string) to look up named contracts.
const directoryABI = `[
	{
		"inputs": [{"internalType": "string", "name": "name", "type": "string"}],
		"name": "getAddress",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "getAllNames",
		"outputs": [{"internalType": "string[]", "name": "", "type": "string[]"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

const relayRegistryABI = `[
	{
		"inputs": [{"internalType": "uint32", "name": "key", "type": "uint32"}],
		"name": "relayKeyToUrl",
		"outputs": [{"internalType": "string", "name": "", "type": "string"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [{"internalType": "uint32", "name": "key", "type": "uint32"}],
		"name": "relayKeyToAddress",
		"outputs": [{"internalType": "address", "name": "", "type": "address"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

type cacheEntry struct {
	url       string
	fetchedAt time.Time
}

type RelayRegistry struct {
	client       *ethclient.Client
	registryAddr common.Address
	registryABI  abi.ABI

	mu    sync.RWMutex
	cache map[uint32]cacheEntry
	ttl   time.Duration
}

// Candidate names the relay registry might be registered under in the directory.
var relayRegistryNames = []string{
	"eigenDARelayRegistry",
	"EigenDARelayRegistry",
	"relayRegistry",
	"RelayRegistry",
}

// NewFromDirectory connects to L1, calls EigenDADirectory.getAddress(name)
// to dynamically resolve the RelayRegistry address, then returns a client for it.
func NewFromDirectory(ethRPCURL string, directoryAddress string) (*RelayRegistry, error) {
	client, err := ethclient.Dial(ethRPCURL)
	if err != nil {
		return nil, fmt.Errorf("connect to eth RPC: %w", err)
	}

	dirABI, err := abi.JSON(strings.NewReader(directoryABI))
	if err != nil {
		return nil, fmt.Errorf("parse directory ABI: %w", err)
	}

	dirAddr := common.HexToAddress(directoryAddress)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// First, list all registered names to find the correct one
	allNames, err := getAllNames(ctx, client, dirABI, dirAddr)
	if err != nil {
		log.Printf("[registry] could not list directory names: %v", err)
	} else {
		log.Printf("[registry] directory has %d registered names: %v", len(allNames), allNames)
	}

	// Try known candidate names
	var registryAddr common.Address
	var found bool
	for _, name := range relayRegistryNames {
		addr, err := getAddress(ctx, client, dirABI, dirAddr, name)
		if err != nil {
			continue
		}
		if addr == (common.Address{}) {
			continue
		}
		registryAddr = addr
		found = true
		log.Printf("[registry] resolved relay registry via name %q → %s", name, addr.Hex())
		break
	}

	// If candidates didn't work, try matching from allNames
	if !found && allNames != nil {
		for _, name := range allNames {
			lower := strings.ToLower(name)
			if strings.Contains(lower, "relay") {
				addr, err := getAddress(ctx, client, dirABI, dirAddr, name)
				if err != nil || addr == (common.Address{}) {
					continue
				}
				registryAddr = addr
				found = true
				log.Printf("[registry] resolved relay registry via directory name %q → %s", name, addr.Hex())
				break
			}
		}
	}

	if !found {
		return nil, fmt.Errorf("could not find relay registry in directory %s (names: %v)", directoryAddress, allNames)
	}

	regABI, err := abi.JSON(strings.NewReader(relayRegistryABI))
	if err != nil {
		return nil, fmt.Errorf("parse relay registry ABI: %w", err)
	}

	return &RelayRegistry{
		client:       client,
		registryAddr: registryAddr,
		registryABI:  regABI,
		cache:        make(map[uint32]cacheEntry),
		ttl:          1 * time.Hour,
	}, nil
}

func getAllNames(ctx context.Context, client *ethclient.Client, dirABI abi.ABI, dirAddr common.Address) ([]string, error) {
	data, err := dirABI.Pack("getAllNames")
	if err != nil {
		return nil, err
	}

	result, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &dirAddr,
		Data: data,
	}, nil)
	if err != nil {
		return nil, err
	}

	values, err := dirABI.Unpack("getAllNames", result)
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("empty result")
	}

	names, ok := values[0].([]string)
	if !ok {
		return nil, fmt.Errorf("unexpected type for names")
	}
	return names, nil
}

func getAddress(ctx context.Context, client *ethclient.Client, dirABI abi.ABI, dirAddr common.Address, name string) (common.Address, error) {
	data, err := dirABI.Pack("getAddress", name)
	if err != nil {
		return common.Address{}, err
	}

	result, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &dirAddr,
		Data: data,
	}, nil)
	if err != nil {
		return common.Address{}, err
	}

	values, err := dirABI.Unpack("getAddress", result)
	if err != nil {
		return common.Address{}, err
	}
	if len(values) == 0 {
		return common.Address{}, fmt.Errorf("empty result")
	}

	addr, ok := values[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected type")
	}
	return addr, nil
}

func (r *RelayRegistry) RegistryAddress() common.Address {
	return r.registryAddr
}

func (r *RelayRegistry) GetRelayURL(ctx context.Context, relayKey uint32) (string, error) {
	r.mu.RLock()
	if entry, ok := r.cache[relayKey]; ok && time.Since(entry.fetchedAt) < r.ttl {
		r.mu.RUnlock()
		return entry.url, nil
	}
	r.mu.RUnlock()

	data, err := r.registryABI.Pack("relayKeyToUrl", relayKey)
	if err != nil {
		return "", fmt.Errorf("pack call: %w", err)
	}

	result, err := r.client.CallContract(ctx, ethereum.CallMsg{
		To:   &r.registryAddr,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("call relayKeyToUrl(%d): %w", relayKey, err)
	}

	values, err := r.registryABI.Unpack("relayKeyToUrl", result)
	if err != nil {
		return "", fmt.Errorf("unpack relayKeyToUrl: %w", err)
	}
	if len(values) == 0 {
		return "", fmt.Errorf("empty result for relay key %d", relayKey)
	}

	url, ok := values[0].(string)
	if !ok {
		return "", fmt.Errorf("unexpected type for relay URL")
	}

	r.mu.Lock()
	r.cache[relayKey] = cacheEntry{url: url, fetchedAt: time.Now()}
	r.mu.Unlock()

	return url, nil
}

func (r *RelayRegistry) Close() {
	r.client.Close()
}

package worker

import (
	"context"
	"encoding/hex"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jyo-o/BONDA/eigenda/internal/contracts"
	"github.com/jyo-o/BONDA/eigenda/internal/db"
)

const (
	ejectionIndexerName = "ejection"
	finalityBlocks      = 64
	maxBlockRange       = 10000
)

type EjectionIndexer struct {
	db        *db.DB
	ethClient *ethclient.Client
	dirAddr   common.Address
	interval  time.Duration
}

func NewEjectionIndexer(database *db.DB, ethClient *ethclient.Client,
	dirAddr common.Address, interval time.Duration) *EjectionIndexer {
	return &EjectionIndexer{
		db:        database,
		ethClient: ethClient,
		dirAddr:   dirAddr,
		interval:  interval,
	}
}

func (e *EjectionIndexer) Name() string { return "ejection-indexer" }

func (e *EjectionIndexer) Run(ctx context.Context) {
	log.Println("[ejection-indexer] started")

	// Resolve EjectionManager
	ejectionAddr, err := contracts.ResolveAddress(ctx, e.ethClient, e.dirAddr, "EJECTION_MANAGER")
	if err != nil {
		log.Printf("[ejection-indexer] failed to resolve EJECTION_MANAGER: %v", err)
		return
	}
	log.Printf("[ejection-indexer] EJECTION_MANAGER=%s", ejectionAddr.Hex())

	// Run once immediately
	e.poll(ctx, ejectionAddr)

	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.poll(ctx, ejectionAddr)
		}
	}
}

func (e *EjectionIndexer) poll(ctx context.Context, ejectionAddr common.Address) {
	cursor, err := e.db.GetIndexerCursor(ctx, ejectionIndexerName)
	if err != nil {
		log.Printf("[ejection-indexer] get cursor: %v", err)
		return
	}

	head, err := e.ethClient.BlockNumber(ctx)
	if err != nil {
		log.Printf("[ejection-indexer] get block number: %v", err)
		return
	}

	safeHead := int64(head) - finalityBlocks
	if safeHead <= cursor {
		return
	}

	fromBlock := cursor + 1
	toBlock := safeHead
	if toBlock-fromBlock > maxBlockRange {
		toBlock = fromBlock + maxBlockRange
	}

	logs, err := e.ethClient.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(fromBlock),
		ToBlock:   big.NewInt(toBlock),
		Addresses: []common.Address{ejectionAddr},
		Topics:    [][]common.Hash{{contracts.OperatorEjectedTopic}},
	})
	if err != nil {
		log.Printf("[ejection-indexer] filter logs [%d-%d]: %v", fromBlock, toBlock, err)
		return
	}

	for _, vLog := range logs {
		// OperatorEjected(bytes32 indexed operatorId, uint8 quorumNumber)
		// Topic[0] = event sig, Topic[1] = operatorId (indexed)
		// Data = quorumNumber (uint8, padded to 32 bytes)
		if len(vLog.Topics) < 2 {
			continue
		}
		operatorID := vLog.Topics[1]
		quorumNumber := 0
		if len(vLog.Data) >= 32 {
			quorumNumber = int(vLog.Data[31])
		}

		// Get block timestamp
		block, blockErr := e.ethClient.BlockByNumber(ctx, big.NewInt(int64(vLog.BlockNumber)))
		eventTime := time.Now()
		if blockErr == nil {
			eventTime = time.Unix(int64(block.Time()), 0)
		}

		if err := e.db.InsertEjectionEvent(ctx, &db.EjectionEvent{
			EventTime:    eventTime,
			BlockNumber:  int64(vLog.BlockNumber),
			TxHash:       vLog.TxHash.Hex(),
			LogIndex:     int(vLog.Index),
			OperatorID:   hex.EncodeToString(operatorID[:8]),
			QuorumNumber: quorumNumber,
		}); err != nil {
			log.Printf("[ejection-indexer] insert event: %v", err)
		}
	}

	if len(logs) > 0 {
		log.Printf("[ejection-indexer] %d ejection events in blocks %d-%d", len(logs), fromBlock, toBlock)
	}

	if err := e.db.UpdateIndexerCursor(ctx, ejectionIndexerName, toBlock); err != nil {
		log.Printf("[ejection-indexer] update cursor: %v", err)
	}
}

package worker

import (
	"context"
	"encoding/hex"
	"log"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/jyo-o/BONDA/eigenda/internal/contracts"
	"github.com/jyo-o/BONDA/eigenda/internal/db"
	"github.com/jyo-o/BONDA/eigenda/internal/operator"
)

type StakeIndexer struct {
	db        *db.DB
	discovery *operator.Discovery
	ethClient *ethclient.Client
	dirAddr   common.Address
	interval  time.Duration
}

func NewStakeIndexer(database *db.DB, discovery *operator.Discovery, ethClient *ethclient.Client,
	dirAddr common.Address, interval time.Duration) *StakeIndexer {
	return &StakeIndexer{
		db:        database,
		discovery: discovery,
		ethClient: ethClient,
		dirAddr:   dirAddr,
		interval:  interval,
	}
}

func (s *StakeIndexer) Name() string { return "stake-indexer" }

func (s *StakeIndexer) Run(ctx context.Context) {
	log.Println("[stake-indexer] started")

	// Resolve STAKE_REGISTRY once
	stakeAddr, err := contracts.ResolveAddress(ctx, s.ethClient, s.dirAddr, "STAKE_REGISTRY")
	if err != nil {
		log.Printf("[stake-indexer] failed to resolve STAKE_REGISTRY: %v", err)
		return
	}
	log.Printf("[stake-indexer] STAKE_REGISTRY=%s", stakeAddr.Hex())

	stakeClient, err := contracts.NewStakeRegistryClient(s.ethClient, stakeAddr)
	if err != nil {
		log.Printf("[stake-indexer] failed to create stake client: %v", err)
		return
	}

	// Run immediately, then on interval
	s.snapshot(ctx, stakeClient)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.snapshot(ctx, stakeClient)
		}
	}
}

func (s *StakeIndexer) snapshot(ctx context.Context, stakeClient *contracts.StakeRegistryClient) {
	operators, err := s.discovery.GetOperators(ctx)
	if err != nil {
		log.Printf("[stake-indexer] get operators: %v", err)
		return
	}

	// Mainnet active quorums: 0, 1, 2
	for quorum := uint8(0); quorum <= 2; quorum++ {
		totalStake := new(big.Int)
		type opStake struct {
			id    [32]byte
			stake *big.Int
		}
		var stakes []opStake

		for _, op := range operators {
			stake, err := stakeClient.GetCurrentStake(ctx, op.OperatorID, quorum)
			if err != nil {
				continue
			}
			if stake.Sign() > 0 {
				stakes = append(stakes, opStake{id: op.OperatorID, stake: stake})
				totalStake.Add(totalStake, stake)
			}
		}

		if totalStake.Sign() == 0 || len(stakes) == 0 {
			continue
		}

		// Compute HHI
		totalFloat := new(big.Float).SetInt(totalStake)
		hhi := 0.0
		for _, os := range stakes {
			pct := new(big.Float).SetInt(os.stake)
			pct.Quo(pct, totalFloat)
			pctF, _ := pct.Float64()
			pctF *= 100 // percentage
			hhi += pctF * pctF
		}

		// Insert snapshot
		if err := s.db.InsertStakeSnapshot(ctx, &db.StakeSnapshot{
			QuorumID:      int(quorum),
			TotalStake:    totalStake.String(),
			OperatorCount: len(stakes),
			HHI:           hhi,
		}); err != nil {
			log.Printf("[stake-indexer] insert snapshot quorum=%d: %v", quorum, err)
		}

		// Insert per-operator
		for _, os := range stakes {
			pct := new(big.Float).SetInt(os.stake)
			pct.Quo(pct, new(big.Float).SetInt(totalStake))
			pctF, _ := pct.Float64()
			if err := s.db.InsertStakeSnapshotOperator(ctx, &db.StakeSnapshotOperator{
				QuorumID:   int(quorum),
				OperatorID: hex.EncodeToString(os.id[:8]),
				Stake:      os.stake.String(),
				StakePct:   pctF * 100,
			}); err != nil {
				log.Printf("[stake-indexer] insert operator quorum=%d: %v", quorum, err)
			}
		}

		log.Printf("[stake-indexer] quorum=%d operators=%d totalStake=%s HHI=%.1f",
			quorum, len(stakes), totalStake.String(), hhi)
	}
}

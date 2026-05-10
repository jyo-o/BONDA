package worker

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/db"
	"github.com/jyo-o/BONDA/eigenda/internal/eigenexplorer"
	"github.com/jyo-o/BONDA/eigenda/internal/operator"
)

type OperatorStatusWorker struct {
	db          *db.DB
	metaClient  *eigenexplorer.Client
	discovery   *operator.Discovery
	interval    time.Duration
}

func NewOperatorStatusWorker(database *db.DB, metaClient *eigenexplorer.Client,
	discovery *operator.Discovery, interval time.Duration) *OperatorStatusWorker {
	return &OperatorStatusWorker{
		db:         database,
		metaClient: metaClient,
		discovery:  discovery,
		interval:   interval,
	}
}

func (w *OperatorStatusWorker) Name() string { return "operator-status" }

func (w *OperatorStatusWorker) Run(ctx context.Context) {
	log.Println("[operator-status] started")

	w.snapshot(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.snapshot(ctx)
		}
	}
}

func (w *OperatorStatusWorker) snapshot(ctx context.Context) {
	operators, err := w.discovery.GetOperators(ctx)
	if err != nil {
		log.Printf("[operator-status] get operators: %v", err)
		return
	}

	resolved := 0
	for _, op := range operators {
		opIDHex := hex.EncodeToString(op.OperatorID[:8])

		// Determine status from blacklist
		status := "active"
		if w.discovery.IsBlacklisted(op.OperatorID) {
			status = "dead"
		}

		// Try to resolve on-chain metadata
		name := ""
		meta, err := w.metaClient.FetchMetadata(ctx, op.OperatorID)
		if err == nil && meta != nil && meta.Name != "" {
			name = meta.Name
			resolved++
		}

		if err := w.db.UpsertOperatorStatus(ctx, &db.OperatorStatus{
			OperatorAddress: opIDHex,
			MetadataName:    name,
			Status:          status,
		}); err != nil {
			log.Printf("[operator-status] upsert %s: %v", opIDHex, err)
		}
	}

	log.Printf("[operator-status] snapshot: %d operators, %d names resolved", len(operators), resolved)
}

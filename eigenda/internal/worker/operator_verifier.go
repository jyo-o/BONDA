package worker

import (
	"context"
	"encoding/hex"
	"log"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/db"
	"github.com/jyo-o/BONDA/eigenda/internal/operator"
)

const minChunksForRecovery = 1024

type OperatorVerifier struct {
	db          *db.DB
	opDiscovery *operator.Discovery
	opClient    *operator.Client
}

func NewOperatorVerifier(database *db.DB, discovery *operator.Discovery, client *operator.Client) *OperatorVerifier {
	return &OperatorVerifier{
		db:          database,
		opDiscovery: discovery,
		opClient:    client,
	}
}

func (v *OperatorVerifier) Name() string { return "operator-verifier" }

func (v *OperatorVerifier) Run(ctx context.Context) {
	if v.opDiscovery == nil || v.opClient == nil {
		log.Println("[operator-verifier] disabled (no operator discovery)")
		return
	}

	// Warm operator cache
	if ops, err := v.opDiscovery.GetOperators(ctx); err != nil {
		log.Printf("[operator-verifier] operator cache warm failed: %v", err)
	} else {
		log.Printf("[operator-verifier] operator cache warmed: %d unique operators", len(ops))
	}

	log.Println("[operator-verifier] started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		unprobed, err := v.db.GetUnprobedOperatorBlobs(ctx, 1)
		if err != nil || len(unprobed) == 0 {
			time.Sleep(2 * time.Second)
			continue
		}

		blob := unprobed[0]
		blobAgeHours := float64(time.Now().UnixNano()-int64(blob.RequestedAt)) / float64(time.Hour)
		v.probeAllOperators(ctx, blob.BlobKey, blobAgeHours)
	}
}

func (v *OperatorVerifier) probeAllOperators(ctx context.Context, blobKey string, blobAgeHours float64) {
	allOperators, err := v.opDiscovery.GetOperators(ctx)
	if err != nil {
		return
	}

	totalChunks := 0
	okCount := 0
	failCount := 0

	for _, op := range allOperators {
		if v.opDiscovery.IsBlacklisted(op.OperatorID) {
			continue
		}

		r := v.opClient.ProbeChunks(ctx, op.Socket, blobKey, 0)
		opIDHex := hex.EncodeToString(op.OperatorID[:8])

		v.opDiscovery.ReportResult(op.OperatorID, r.Success)

		v.db.InsertOperatorProbe(ctx, &db.OperatorProbeResult{
			BlobKey: blobKey, BlobAgeHours: blobAgeHours,
			OperatorID: opIDHex, OperatorSocket: op.Socket,
			QuorumID: 0, Success: r.Success,
			LatencyMs: r.LatencyMs, ChunksReturned: r.ChunksReturned,
			ErrorMessage: r.Error,
		})

		if r.Success {
			okCount++
			totalChunks += r.ChunksReturned
		} else {
			failCount++
		}
	}

	tag := "RECOVERABLE"
	if totalChunks < minChunksForRecovery {
		tag = "AT_RISK"
	}
	logKey := blobKey
	if len(logKey) > 12 {
		logKey = logKey[:12]
	}
	log.Printf("[recovery] blob=%s operators=%d/%d chunks=%d/%d %s",
		logKey, okCount, okCount+failCount, totalChunks, minChunksForRecovery, tag)
}

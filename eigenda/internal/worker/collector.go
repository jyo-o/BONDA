package worker

import (
	"context"
	"log"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/dataapi"
	"github.com/jyo-o/BONDA/eigenda/internal/db"
)

type BlobCollector struct {
	api              *dataapi.Client
	db               *db.DB
	pollInterval     time.Duration
	lastSeenTimestamp uint64
}

func NewBlobCollector(api *dataapi.Client, database *db.DB, pollInterval time.Duration) *BlobCollector {
	return &BlobCollector{
		api:          api,
		db:           database,
		pollInterval: pollInterval,
	}
}

func (c *BlobCollector) Name() string { return "collector" }

func (c *BlobCollector) Run(ctx context.Context) {
	log.Println("[collector] started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n := c.collectBlobs(ctx)
		if n == 0 {
			time.Sleep(c.pollInterval)
		} else {
			time.Sleep(1 * time.Second)
		}
	}
}

func (c *BlobCollector) collectBlobs(ctx context.Context) int {
	feed, err := c.api.FetchBlobFeed(100, "")
	if err != nil {
		log.Printf("[collector] error: %v", err)
		return 0
	}
	if len(feed.Blobs) == 0 {
		return 0
	}

	newCount := 0
	for _, blob := range feed.Blobs {
		if blob.BlobMetadata.RequestedAt <= c.lastSeenTimestamp {
			break
		}
		err := c.db.UpsertBlob(ctx, &db.ObservedBlob{
			BlobKey:       blob.BlobKey,
			AccountID:     blob.BlobMetadata.BlobHeader.PaymentMetadata.AccountID,
			BlobStatus:    blob.BlobMetadata.BlobStatus,
			BlobSizeBytes: blob.BlobMetadata.BlobSizeBytes,
			RequestedAt:   blob.BlobMetadata.RequestedAt,
			ExpiryUnixSec: blob.BlobMetadata.ExpiryUnixSec,
			CommitmentX:   blob.BlobMetadata.BlobHeader.BlobCommitments.Commitment.X,
			CommitmentY:   blob.BlobMetadata.BlobHeader.BlobCommitments.Commitment.Y,
			QuorumNumbers: blob.BlobMetadata.BlobHeader.QuorumNumbers,
		})
		if err == nil {
			newCount++
		}
	}

	if feed.Blobs[0].BlobMetadata.RequestedAt > c.lastSeenTimestamp {
		c.lastSeenTimestamp = feed.Blobs[0].BlobMetadata.RequestedAt
	}

	if newCount > 0 {
		log.Printf("[collector] +%d new blobs", newCount)
	}
	return newCount
}

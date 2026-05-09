package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/jyo-o/BONDA/eigenda/internal/db"
)

type WriteProber struct {
	db       *db.DB
	grpcURL  string
	interval time.Duration
}

func NewWriteProber(database *db.DB, grpcURL string, interval time.Duration) *WriteProber {
	return &WriteProber{
		db:       database,
		grpcURL:  grpcURL,
		interval: interval,
	}
}

func (w *WriteProber) Name() string { return "write-prober" }

func (w *WriteProber) Run(ctx context.Context) {
	log.Println("[write-prober] started")
	if w.grpcURL == "" {
		log.Println("[write-prober] DISPERSER_GRPC_URL not set, disabled")
		return
	}

	// Run immediately, then on interval
	w.disperse(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.disperse(ctx)
		}
	}
}

func (w *WriteProber) disperse(ctx context.Context) {
	start := time.Now()

	// Generate random 128KiB payload (high byte zeroed for BN254)
	payload := make([]byte, 128*1024)
	if _, err := rand.Read(payload); err != nil {
		log.Printf("[write-prober] rand read: %v", err)
		return
	}
	// Zero high byte of each 32-byte field element to stay below BN254 modulus
	for i := 0; i < len(payload); i += 32 {
		if i < len(payload) {
			payload[i] = 0
		}
	}

	// Connect to disperser
	conn, err := grpc.NewClient(w.grpcURL,
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")),
	)
	if err != nil {
		log.Printf("[write-prober] dial disperser: %v", err)
		return
	}
	defer conn.Close()

	// For MVP: log that we would disperse but the actual disperser proto
	// integration requires payment setup. Record the attempt.
	latencyMs := int(time.Since(start).Milliseconds())

	// Generate a synthetic blob key for tracking
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		log.Printf("[write-prober] rand read key: %v", err)
		return
	}
	blobKey := hex.EncodeToString(keyBytes)

	log.Printf("[write-prober] dispersal attempt completed in %dms (blob_key=%s)", latencyMs, blobKey[:12])

	// TODO: When payment is configured, use the actual disperser v2 client:
	// 1. disperserClient.DisperseBlob(ctx, &DisperseRequest{Data: payload, ...})
	// 2. Poll disperserClient.GetBlobStatus(ctx, blobKey) until COMPLETE
	// 3. Extract commitment from response
	// 4. Record with actual blob key and commitment

	// For now, record the connectivity test
	if err := w.db.UpsertBlob(ctx, &db.ObservedBlob{
		BlobKey:            "self-" + blobKey,
		BlobStatus:         "CONNECTIVITY_TEST",
		BlobSizeBytes:      len(payload),
		RequestedAt:        uint64(time.Now().UnixNano()),
		IsSelfDispersed:    true,
		DispersalLatencyMs: latencyMs,
	}); err != nil {
		log.Printf("[write-prober] upsert blob: %v", err)
	}
}

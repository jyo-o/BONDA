package worker

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/db"
)

type WriteProber struct {
	db       *db.DB
	proxyURL string
	interval time.Duration
	client   *http.Client
}

func NewWriteProber(database *db.DB, proxyURL string, interval time.Duration) *WriteProber {
	return &WriteProber{
		db:       database,
		proxyURL: proxyURL,
		interval: interval,
		client:   &http.Client{Timeout: 5 * time.Minute},
	}
}

func (w *WriteProber) Name() string { return "write-prober" }

func (w *WriteProber) Run(ctx context.Context) {
	log.Println("[write-prober] started")
	if w.proxyURL == "" {
		log.Println("[write-prober] EIGENDA_PROXY_URL not set, disabled")
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

	// Generate random 128KiB payload (high byte zeroed for BN254 modulus)
	payload := make([]byte, 128*1024)
	if _, err := rand.Read(payload); err != nil {
		log.Printf("[write-prober] rand read: %v", err)
		return
	}
	for i := 0; i < len(payload); i += 32 {
		payload[i] = 0
	}

	// Submit to eigenda-proxy
	url := fmt.Sprintf("%s/put?commitment_mode=standard", w.proxyURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		log.Printf("[write-prober] create request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := w.client.Do(req)
	if err != nil {
		log.Printf("[write-prober] proxy request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	latencyMs := int(time.Since(start).Milliseconds())

	if resp.StatusCode != 200 {
		log.Printf("[write-prober] proxy returned %d: %s (latency=%dms)", resp.StatusCode, string(body), latencyMs)
		return
	}

	// Response body is the DA certificate (commitment bytes)
	// Use hex-encoded cert as blob key for tracking
	blobKey := hex.EncodeToString(body)
	if len(blobKey) > 64 {
		blobKey = blobKey[:64]
	}

	log.Printf("[write-prober] dispersal OK latency=%dms cert=%s", latencyMs, blobKey[:12])

	if err := w.db.UpsertBlob(ctx, &db.ObservedBlob{
		BlobKey:            blobKey,
		BlobStatus:         "CONFIRMED",
		BlobSizeBytes:      len(payload),
		RequestedAt:        uint64(time.Now().UnixNano()),
		IsSelfDispersed:    true,
		DispersalLatencyMs: latencyMs,
	}); err != nil {
		log.Printf("[write-prober] upsert blob: %v", err)
	}
}

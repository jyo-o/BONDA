package worker

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/dataapi"
	"github.com/jyo-o/BONDA/eigenda/internal/db"
)

type WriteProber struct {
	db        *db.DB
	api       *dataapi.Client
	proxyURL  string
	accountID string
	interval  time.Duration
	client    *http.Client
}

func NewWriteProber(database *db.DB, api *dataapi.Client, proxyURL string, accountID string, interval time.Duration) *WriteProber {
	return &WriteProber{
		db:        database,
		api:       api,
		proxyURL:  proxyURL,
		accountID: strings.ToLower(accountID),
		interval:  interval,
		client:    &http.Client{Timeout: 5 * time.Minute},
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

	log.Printf("[write-prober] dispersal OK latency=%dms", latencyMs)

	// Find the actual blob from DataAPI by matching our account
	blob := w.findSelfBlob(ctx)
	if blob == nil {
		log.Printf("[write-prober] dispersal succeeded but could not find blob in DataAPI")
		return
	}

	var cumPayment *string
	if cp := blob.BlobMetadata.BlobHeader.PaymentMetadata.CumulativePayment; cp != 0 {
		s := fmt.Sprintf("%d", cp)
		cumPayment = &s
	}

	if err := w.db.UpsertBlob(ctx, &db.ObservedBlob{
		BlobKey:            blob.BlobKey,
		BlobStatus:         "CONFIRMED",
		BlobSizeBytes:      len(payload),
		RequestedAt:        uint64(time.Now().UnixNano()),
		IsSelfDispersed:    true,
		DispersalLatencyMs: latencyMs,
		CumulativePayment:  cumPayment,
	}); err != nil {
		log.Printf("[write-prober] upsert blob: %v", err)
	}

	log.Printf("[write-prober] recorded blob_key=%s cumulative_payment=%v", blob.BlobKey[:16], cumPayment)
}

// findSelfBlob queries DataAPI for the most recent blob from our account.
func (w *WriteProber) findSelfBlob(ctx context.Context) *dataapi.BlobEntry {
	// Retry a few times — there may be a short delay before our blob appears
	for i := 0; i < 5; i++ {
		feed, err := w.api.FetchBlobFeed(20, "")
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		for _, blob := range feed.Blobs {
			acct := strings.ToLower(blob.BlobMetadata.BlobHeader.PaymentMetadata.AccountID)
			if acct == w.accountID {
				return &blob
			}
		}
		time.Sleep(3 * time.Second)
	}
	return nil
}

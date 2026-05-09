package worker

import (
	"context"
	"log"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/dataapi"
	"github.com/jyo-o/BONDA/eigenda/internal/db"
	"github.com/jyo-o/BONDA/eigenda/internal/registry"
	"github.com/jyo-o/BONDA/eigenda/internal/relay"
)

type ageGroup struct {
	hoursAgo    float64
	windowHours float64
	limit       int
}

var ageGroups = []ageGroup{
	{hoursAgo: 0.083, windowHours: 0.033, limit: 3},  // ~5min
	{hoursAgo: 0.5, windowHours: 0.17, limit: 3},     // 30min
	{hoursAgo: 2, windowHours: 0.5, limit: 3},         // 2h
	{hoursAgo: 8, windowHours: 1, limit: 3},            // 8h
	{hoursAgo: 24, windowHours: 2, limit: 5},           // 1d
	{hoursAgo: 72, windowHours: 4, limit: 5},            // 3d
	{hoursAgo: 168, windowHours: 4, limit: 5},           // 7d
	{hoursAgo: 312, windowHours: 4, limit: 5},           // 13d
	{hoursAgo: 336, windowHours: 4, limit: 3},           // 14d
	{hoursAgo: 360, windowHours: 4, limit: 3},           // 15d
}

type Reverifier struct {
	api      *dataapi.Client
	db       *db.DB
	relay    *relay.Client
	registry *registry.RelayRegistry
	interval time.Duration
}

func NewReverifier(api *dataapi.Client, database *db.DB, relayClient *relay.Client,
	reg *registry.RelayRegistry, interval time.Duration) *Reverifier {
	return &Reverifier{
		api:      api,
		db:       database,
		relay:    relayClient,
		registry: reg,
		interval: interval,
	}
}

func (r *Reverifier) Name() string { return "reverifier" }

func (r *Reverifier) Run(ctx context.Context) {
	log.Println("[reverifier] started")
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	// Run immediately on start
	r.ageBasedReverify(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.ageBasedReverify(ctx)
		}
	}
}

func (r *Reverifier) ageBasedReverify(ctx context.Context) {
	for _, ag := range ageGroups {
		aged, err := r.db.GetAgedBlobKeys(ctx, ag.hoursAgo, ag.windowHours, ag.limit)
		if err != nil || len(aged) == 0 {
			continue
		}
		log.Printf("[reverifier] %d blobs at ~%.1fh", len(aged), ag.hoursAgo)
		for _, ab := range aged {
			r.reprobeBlob(ctx, ab.BlobKey, ab.RequestedAt)
		}
	}
}

func (r *Reverifier) reprobeBlob(ctx context.Context, blobKey string, requestedAt uint64) {
	if r.registry == nil {
		return
	}
	blobAgeHours := float64(time.Now().UnixNano()-int64(requestedAt)) / float64(time.Hour)

	cert, err := r.api.FetchCertificate(blobKey)
	if err != nil {
		return
	}
	if len(cert.BlobCertificate.RelayKeys) == 0 {
		return
	}
	for _, relayKey := range cert.BlobCertificate.RelayKeys {
		relayURL, err := r.registry.GetRelayURL(ctx, relayKey)
		if err != nil {
			continue
		}
		result := r.relay.GetBlob(ctx, relayURL, blobKey)
		r.db.InsertProbeResult(ctx, &db.ProbeResult{
			BlobKey: blobKey, BlobAgeHours: blobAgeHours,
			RelayKey: int(relayKey), Success: result.Success,
			LatencyMs: result.LatencyMs, ErrorMessage: result.Error,
			DataSizeBytes: result.DataSizeBytes,
		})
	}
}

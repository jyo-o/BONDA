package worker

import (
	"context"
	"log"
	"runtime"
	"time"

	"github.com/jyo-o/BONDA/eigenda/internal/db"
)

type HealthRecorder struct {
	db        *db.DB
	interval  time.Duration
	startTime time.Time
}

func NewHealthRecorder(database *db.DB, interval time.Duration) *HealthRecorder {
	return &HealthRecorder{
		db:        database,
		interval:  interval,
		startTime: time.Now(),
	}
}

func (h *HealthRecorder) Name() string { return "health-recorder" }

func (h *HealthRecorder) Run(ctx context.Context) {
	log.Println("[health-recorder] started")
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.record(ctx)
		}
	}
}

func (h *HealthRecorder) record(ctx context.Context) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	stats := h.db.DBStats()

	err := h.db.InsertProberHealth(ctx, &db.ProberHealth{
		Goroutines:       runtime.NumGoroutine(),
		HeapAllocMB:      float64(mem.HeapAlloc) / 1024 / 1024,
		HeapSysMB:        float64(mem.HeapSys) / 1024 / 1024,
		DBOpenConns:      stats.OpenConnections,
		DBInUse:          stats.InUse,
		DBIdle:           stats.Idle,
		DBWaitCount:      stats.WaitCount,
		DBWaitDurationMs: stats.WaitDuration.Milliseconds(),
		UptimeSeconds:    int64(time.Since(h.startTime).Seconds()),
	})
	if err != nil {
		log.Printf("[health-recorder] insert error: %v", err)
	}
}

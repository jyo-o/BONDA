package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"
)

type Server struct {
	db     *sql.DB
	addr   string
	server *http.Server
}

func NewServer(dbConn *sql.DB, addr string) *Server {
	return &Server{db: dbConn, addr: addr}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/survival", s.handleSurvival)
	mux.HandleFunc("/api/operators", s.handleOperators)
	mux.HandleFunc("/api/relays", s.handleRelays)
	mux.HandleFunc("/api/hhi", s.handleHHI)
	mux.HandleFunc("/api/ejections", s.handleEjections)
	mux.HandleFunc("/api/probes", s.handleProbes)

	s.server = &http.Server{Addr: s.addr, Handler: mux}
	log.Printf("[api] listening on %s", s.addr)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	type statusResp struct {
		RelaySuccessRate    *float64 `json:"relay_success_rate"`
		RelayAvgLatency     *float64 `json:"relay_avg_latency_ms"`
		RelayProbes         int      `json:"relay_probes_1h"`
		OperatorSuccessRate *float64 `json:"operator_success_rate"`
		OperatorAvgLatency  *float64 `json:"operator_avg_latency_ms"`
		OperatorProbes      int      `json:"operator_probes_1h"`
		TotalBlobs          int      `json:"total_blobs"`
		RecentBlobs         int      `json:"recent_blobs_1h"`
		Status              string   `json:"status"`
	}
	var resp statusResp

	// Relay stats last 1h
	_ = s.db.QueryRowContext(r.Context(), `
		SELECT
			COUNT(*) FILTER (WHERE success)::float / NULLIF(COUNT(*)::float, 0) * 100,
			AVG(latency_ms) FILTER (WHERE success),
			COUNT(*)
		FROM eigenda.retrieval_probes
		WHERE probe_time > NOW() - INTERVAL '1 hour' AND relay_key >= 0
	`).Scan(&resp.RelaySuccessRate, &resp.RelayAvgLatency, &resp.RelayProbes)

	// Operator stats last 1h
	_ = s.db.QueryRowContext(r.Context(), `
		SELECT
			COUNT(*) FILTER (WHERE success)::float / NULLIF(COUNT(*)::float, 0) * 100,
			AVG(latency_ms) FILTER (WHERE success),
			COUNT(*)
		FROM eigenda.operator_probes
		WHERE probe_time > NOW() - INTERVAL '1 hour'
	`).Scan(&resp.OperatorSuccessRate, &resp.OperatorAvgLatency, &resp.OperatorProbes)

	// Blob counts
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM eigenda.observed_blobs`).Scan(&resp.TotalBlobs)
	_ = s.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM eigenda.observed_blobs WHERE first_observed_at > NOW() - INTERVAL '1 hour'`).Scan(&resp.RecentBlobs)

	// Status determination
	resp.Status = "healthy"
	if resp.RelaySuccessRate != nil && *resp.RelaySuccessRate < 95 {
		resp.Status = "down"
	} else if resp.RelaySuccessRate != nil && *resp.RelaySuccessRate < 99 {
		resp.Status = "degraded"
	}

	writeJSON(w, resp)
}

func (s *Server) handleSurvival(w http.ResponseWriter, r *http.Request) {
	type bucket struct {
		AgeBucketHours float64 `json:"age_bucket_hours"`
		Total          int     `json:"total"`
		Successes      int     `json:"successes"`
		SuccessRate    float64 `json:"success_rate"`
		CILower        float64 `json:"ci_lower"`
		CIUpper        float64 `json:"ci_upper"`
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT
			FLOOR(blob_age_hours / 12) * 12 AS age_bucket,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE success) AS successes,
			COUNT(*) FILTER (WHERE success)::float / NULLIF(COUNT(*)::float, 0) * 100
		FROM eigenda.retrieval_probes
		WHERE blob_age_hours IS NOT NULL AND blob_age_hours >= 0
		GROUP BY age_bucket
		ORDER BY age_bucket
	`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var buckets []bucket
	z := 1.96
	for rows.Next() {
		var b bucket
		if err := rows.Scan(&b.AgeBucketHours, &b.Total, &b.Successes, &b.SuccessRate); err != nil {
			continue
		}
		if b.Total >= 10 {
			p := float64(b.Successes) / float64(b.Total)
			n := float64(b.Total)
			denom := 1 + z*z/n
			center := (p + z*z/(2*n)) / denom
			margin := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denom
			b.CILower = math.Max(0, (center-margin)*100)
			b.CIUpper = math.Min(100, (center+margin)*100)
		}
		buckets = append(buckets, b)
	}
	writeJSON(w, buckets)
}

func (s *Server) handleOperators(w http.ResponseWriter, r *http.Request) {
	type opStat struct {
		OperatorID  string   `json:"operator_id"`
		Socket      string   `json:"socket"`
		Total       int      `json:"total"`
		Successes   int      `json:"successes"`
		SuccessRate float64  `json:"success_rate"`
		AvgLatency  *float64 `json:"avg_latency_ms"`
		AvgChunks   *float64 `json:"avg_chunks"`
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT operator_id, operator_socket,
			COUNT(*), COUNT(*) FILTER (WHERE success),
			COUNT(*) FILTER (WHERE success)::float / NULLIF(COUNT(*)::float, 0) * 100,
			AVG(latency_ms) FILTER (WHERE success),
			AVG(chunks_returned) FILTER (WHERE success)
		FROM eigenda.operator_probes
		GROUP BY operator_id, operator_socket
		ORDER BY COUNT(*) FILTER (WHERE success)::float / NULLIF(COUNT(*)::float, 0) ASC
	`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var ops []opStat
	for rows.Next() {
		var o opStat
		if err := rows.Scan(&o.OperatorID, &o.Socket, &o.Total, &o.Successes, &o.SuccessRate, &o.AvgLatency, &o.AvgChunks); err != nil {
			continue
		}
		ops = append(ops, o)
	}
	writeJSON(w, ops)
}

func (s *Server) handleRelays(w http.ResponseWriter, r *http.Request) {
	type relayStat struct {
		RelayKey    int      `json:"relay_key"`
		Total       int      `json:"total"`
		Successes   int      `json:"successes"`
		SuccessRate float64  `json:"success_rate"`
		AvgLatency  *float64 `json:"avg_latency_ms"`
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT relay_key, COUNT(*), COUNT(*) FILTER (WHERE success),
			COUNT(*) FILTER (WHERE success)::float / NULLIF(COUNT(*)::float, 0) * 100,
			AVG(latency_ms) FILTER (WHERE success)
		FROM eigenda.retrieval_probes
		WHERE relay_key >= 0
		GROUP BY relay_key ORDER BY relay_key
	`)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var relays []relayStat
	for rows.Next() {
		var rs relayStat
		if err := rows.Scan(&rs.RelayKey, &rs.Total, &rs.Successes, &rs.SuccessRate, &rs.AvgLatency); err != nil {
			continue
		}
		relays = append(relays, rs)
	}
	writeJSON(w, relays)
}

func (s *Server) handleHHI(w http.ResponseWriter, r *http.Request) {
	hours := 168 // default 7 days
	if h := r.URL.Query().Get("hours"); h != "" {
		if v, err := strconv.Atoi(h); err == nil {
			hours = v
		}
	}

	type hhiPoint struct {
		SnapshotTime  time.Time `json:"snapshot_time"`
		QuorumID      int       `json:"quorum_id"`
		HHI           float64   `json:"hhi"`
		OperatorCount int       `json:"operator_count"`
		TotalStake    string    `json:"total_stake"`
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT snapshot_time, quorum_id, hhi, operator_count, total_stake
		FROM eigenda.stake_snapshots
		WHERE snapshot_time > NOW() - make_interval(hours => $1)
		ORDER BY snapshot_time DESC
	`, hours)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var points []hhiPoint
	for rows.Next() {
		var p hhiPoint
		if err := rows.Scan(&p.SnapshotTime, &p.QuorumID, &p.HHI, &p.OperatorCount, &p.TotalStake); err != nil {
			continue
		}
		points = append(points, p)
	}
	writeJSON(w, points)
}

func (s *Server) handleEjections(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 500 {
			limit = v
		}
	}

	type ejection struct {
		EventTime    time.Time `json:"event_time"`
		BlockNumber  int64     `json:"block_number"`
		TxHash       string    `json:"tx_hash"`
		OperatorID   string    `json:"operator_id"`
		QuorumNumber int       `json:"quorum_number"`
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT event_time, block_number, tx_hash, operator_id, quorum_number
		FROM eigenda.ejection_events
		ORDER BY event_time DESC
		LIMIT $1
	`, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var ejections []ejection
	for rows.Next() {
		var e ejection
		if err := rows.Scan(&e.EventTime, &e.BlockNumber, &e.TxHash, &e.OperatorID, &e.QuorumNumber); err != nil {
			continue
		}
		ejections = append(ejections, e)
	}
	writeJSON(w, ejections)
}

func (s *Server) handleProbes(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 200 {
			limit = v
		}
	}

	type probe struct {
		ProbeTime    time.Time `json:"probe_time"`
		BlobKey      string    `json:"blob_key"`
		BlobAgeHours *float64  `json:"blob_age_hours"`
		RelayKey     int       `json:"relay_key"`
		Success      bool      `json:"success"`
		LatencyMs    *int      `json:"latency_ms"`
		ErrorMessage *string   `json:"error_message"`
		DataSize     *int      `json:"data_size_bytes"`
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT probe_time, blob_key, blob_age_hours, relay_key, success,
			latency_ms, error_message, data_size_bytes
		FROM eigenda.retrieval_probes
		ORDER BY probe_time DESC
		LIMIT $1
	`, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	var probes []probe
	for rows.Next() {
		var p probe
		if err := rows.Scan(&p.ProbeTime, &p.BlobKey, &p.BlobAgeHours, &p.RelayKey,
			&p.Success, &p.LatencyMs, &p.ErrorMessage, &p.DataSize); err != nil {
			continue
		}
		probes = append(probes, p)
	}
	writeJSON(w, probes)
}

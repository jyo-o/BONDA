package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type ObservedBlob struct {
	BlobKey            string
	AccountID          string
	BlobStatus         string
	BlobSizeBytes      int
	RequestedAt        uint64
	ExpiryUnixSec      uint64
	CommitmentX        string
	CommitmentY        string
	QuorumNumbers      string
	IsSelfDispersed    bool
	DispersalLatencyMs int
}

type ProbeResult struct {
	BlobKey       string
	BlobAgeHours  float64
	RelayKey      int
	Success       bool
	LatencyMs     int
	ErrorMessage  string
	DataSizeBytes int
	KZGVerified   *bool
	KZGError      string
}

type AttestationSnapshot struct {
	BlobKey               string
	QuorumNumber          int
	TotalNonSigners       int
	SigningStakePercentage float64
}

type OperatorProbeResult struct {
	BlobKey        string
	BlobAgeHours   float64
	OperatorID     string
	OperatorSocket string
	QuorumID       int
	Success        bool
	LatencyMs      int
	ChunksReturned int
	ErrorMessage   string
}

type StakeSnapshot struct {
	QuorumID      int
	TotalStake    string
	OperatorCount int
	HHI           float64
}

type StakeSnapshotOperator struct {
	QuorumID   int
	OperatorID string
	Stake      string
	StakePct   float64
}

type EjectionEvent struct {
	EventTime    time.Time
	BlockNumber  int64
	TxHash       string
	LogIndex     int
	OperatorID   string
	QuorumNumber int
}

type AgedBlobKey struct {
	BlobKey     string
	RequestedAt uint64
}

type DB struct {
	conn *sql.DB
}

func New(databaseURL string) (*DB, error) {
	conn, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(5)
	conn.SetConnMaxLifetime(5 * time.Minute)

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &DB{conn: conn}, nil
}

func (d *DB) RunMigrations(ctx context.Context) error {
	migrations := `
CREATE SCHEMA IF NOT EXISTS eigenda;

CREATE TABLE IF NOT EXISTS eigenda.observed_blobs (
    blob_key         VARCHAR(128) PRIMARY KEY,
    account_id       VARCHAR(64),
    blob_status      VARCHAR(32),
    blob_size_bytes  INTEGER,
    requested_at     BIGINT,
    expiry_unix_sec  BIGINT,
    commitment_x     TEXT,
    commitment_y     TEXT,
    quorum_numbers   TEXT,
    is_self_dispersed BOOLEAN DEFAULT FALSE,
    dispersal_latency_ms INTEGER,
    first_observed_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS eigenda.indexer_cursors (
    indexer_name     VARCHAR(64) PRIMARY KEY,
    last_block       BIGINT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS eigenda.retrieval_probes (
    probe_time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    blob_key         VARCHAR(128) NOT NULL,
    blob_age_hours   FLOAT,
    relay_key        INTEGER,
    success          BOOLEAN,
    latency_ms       INTEGER,
    error_message    TEXT,
    data_size_bytes  INTEGER,
    kzg_verified     BOOLEAN,
    kzg_error        TEXT
);

CREATE TABLE IF NOT EXISTS eigenda.operator_probes (
    probe_time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    blob_key         VARCHAR(128) NOT NULL,
    blob_age_hours   FLOAT,
    operator_id      VARCHAR(128),
    operator_socket  TEXT,
    quorum_id        INTEGER,
    success          BOOLEAN,
    latency_ms       INTEGER,
    chunks_returned  INTEGER,
    error_message    TEXT
);

CREATE TABLE IF NOT EXISTS eigenda.attestation_snapshots (
    snapshot_time            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    blob_key                 VARCHAR(128) NOT NULL,
    quorum_number            INTEGER,
    total_nonsigners         INTEGER,
    signing_stake_percentage FLOAT
);

CREATE TABLE IF NOT EXISTS eigenda.stake_snapshots (
    snapshot_time    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    quorum_id        INTEGER NOT NULL,
    total_stake      NUMERIC,
    operator_count   INTEGER,
    hhi              FLOAT
);

CREATE TABLE IF NOT EXISTS eigenda.stake_snapshot_operators (
    snapshot_time    TIMESTAMPTZ NOT NULL,
    quorum_id        INTEGER NOT NULL,
    operator_id      VARCHAR(128),
    stake            NUMERIC,
    stake_pct        FLOAT
);

CREATE TABLE IF NOT EXISTS eigenda.ejection_events (
    event_time       TIMESTAMPTZ NOT NULL,
    block_number     BIGINT NOT NULL,
    tx_hash          VARCHAR(66),
    log_index        INTEGER,
    operator_id      VARCHAR(128),
    quorum_number    INTEGER,
    UNIQUE(tx_hash, log_index)
);

CREATE INDEX IF NOT EXISTS idx_rp_blob_key ON eigenda.retrieval_probes(blob_key);
CREATE INDEX IF NOT EXISTS idx_rp_age ON eigenda.retrieval_probes(blob_age_hours);
CREATE INDEX IF NOT EXISTS idx_op_blob ON eigenda.operator_probes(blob_key);
CREATE INDEX IF NOT EXISTS idx_op_ts ON eigenda.operator_probes(probe_time);
`
	_, err := d.conn.ExecContext(ctx, migrations)
	return err
}

func (d *DB) UpsertBlob(ctx context.Context, b *ObservedBlob) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.observed_blobs (blob_key, account_id, blob_status, blob_size_bytes,
			requested_at, expiry_unix_sec, commitment_x, commitment_y, quorum_numbers,
			is_self_dispersed, dispersal_latency_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (blob_key) DO UPDATE SET
			is_self_dispersed = EXCLUDED.is_self_dispersed OR eigenda.observed_blobs.is_self_dispersed,
			dispersal_latency_ms = COALESCE(NULLIF(EXCLUDED.dispersal_latency_ms, 0), eigenda.observed_blobs.dispersal_latency_ms)`,
		b.BlobKey, b.AccountID, b.BlobStatus, b.BlobSizeBytes,
		b.RequestedAt, b.ExpiryUnixSec, b.CommitmentX, b.CommitmentY, b.QuorumNumbers,
		b.IsSelfDispersed, b.DispersalLatencyMs,
	)
	return err
}

func (d *DB) InsertProbeResult(ctx context.Context, p *ProbeResult) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.retrieval_probes (blob_key, blob_age_hours, relay_key, success,
			latency_ms, error_message, data_size_bytes, kzg_verified, kzg_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		p.BlobKey, p.BlobAgeHours, p.RelayKey, p.Success,
		p.LatencyMs, p.ErrorMessage, p.DataSizeBytes, p.KZGVerified, p.KZGError,
	)
	return err
}

func (d *DB) InsertAttestation(ctx context.Context, a *AttestationSnapshot) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.attestation_snapshots (blob_key, quorum_number,
			total_nonsigners, signing_stake_percentage)
		VALUES ($1, $2, $3, $4)`,
		a.BlobKey, a.QuorumNumber,
		a.TotalNonSigners, a.SigningStakePercentage,
	)
	return err
}

func (d *DB) InsertOperatorProbe(ctx context.Context, p *OperatorProbeResult) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.operator_probes (blob_key, blob_age_hours, operator_id, operator_socket,
			quorum_id, success, latency_ms, chunks_returned, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		p.BlobKey, p.BlobAgeHours, p.OperatorID, p.OperatorSocket,
		p.QuorumID, p.Success, p.LatencyMs, p.ChunksReturned, p.ErrorMessage,
	)
	return err
}

func (d *DB) GetUnprobedBlobs(ctx context.Context, limit int) ([]AgedBlobKey, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT ob.blob_key, ob.requested_at FROM eigenda.observed_blobs ob
		LEFT JOIN eigenda.retrieval_probes rp ON ob.blob_key = rp.blob_key
		WHERE rp.blob_key IS NULL
		ORDER BY ob.requested_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AgedBlobKey
	for rows.Next() {
		var bk AgedBlobKey
		if err := rows.Scan(&bk.BlobKey, &bk.RequestedAt); err != nil {
			return nil, err
		}
		results = append(results, bk)
	}
	return results, rows.Err()
}

func (d *DB) GetUnprobedOperatorBlobs(ctx context.Context, limit int) ([]AgedBlobKey, error) {
	rows, err := d.conn.QueryContext(ctx, `
		SELECT ob.blob_key, ob.requested_at FROM eigenda.observed_blobs ob
		LEFT JOIN eigenda.operator_probes op ON ob.blob_key = op.blob_key
		WHERE op.blob_key IS NULL
		ORDER BY ob.requested_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AgedBlobKey
	for rows.Next() {
		var bk AgedBlobKey
		if err := rows.Scan(&bk.BlobKey, &bk.RequestedAt); err != nil {
			return nil, err
		}
		results = append(results, bk)
	}
	return results, rows.Err()
}

func (d *DB) GetAgedBlobKeys(ctx context.Context, hoursAgo float64, windowHours float64, limit int) ([]AgedBlobKey, error) {
	nowNano := time.Now().UnixNano()
	targetNano := nowNano - int64(hoursAgo*float64(time.Hour))
	windowNano := int64(windowHours * float64(time.Hour))

	rows, err := d.conn.QueryContext(ctx, `
		SELECT blob_key, requested_at FROM eigenda.observed_blobs
		WHERE requested_at BETWEEN $1 AND $2
		ORDER BY RANDOM()
		LIMIT $3`,
		targetNano-windowNano, targetNano+windowNano, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []AgedBlobKey
	for rows.Next() {
		var bk AgedBlobKey
		if err := rows.Scan(&bk.BlobKey, &bk.RequestedAt); err != nil {
			return nil, err
		}
		results = append(results, bk)
	}
	return results, rows.Err()
}

func (d *DB) InsertStakeSnapshot(ctx context.Context, s *StakeSnapshot) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.stake_snapshots (quorum_id, total_stake, operator_count, hhi)
		VALUES ($1, $2, $3, $4)`,
		s.QuorumID, s.TotalStake, s.OperatorCount, s.HHI,
	)
	return err
}

func (d *DB) InsertStakeSnapshotOperator(ctx context.Context, s *StakeSnapshotOperator) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.stake_snapshot_operators (quorum_id, operator_id, stake, stake_pct)
		VALUES ($1, $2, $3, $4)`,
		s.QuorumID, s.OperatorID, s.Stake, s.StakePct,
	)
	return err
}

func (d *DB) InsertEjectionEvent(ctx context.Context, e *EjectionEvent) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.ejection_events (event_time, block_number, tx_hash, log_index,
			operator_id, quorum_number)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tx_hash, log_index) DO NOTHING`,
		e.EventTime, e.BlockNumber, e.TxHash, e.LogIndex,
		e.OperatorID, e.QuorumNumber,
	)
	return err
}

func (d *DB) GetIndexerCursor(ctx context.Context, indexerName string) (int64, error) {
	var lastBlock int64
	err := d.conn.QueryRowContext(ctx, `
		SELECT last_block FROM eigenda.indexer_cursors WHERE indexer_name = $1`,
		indexerName,
	).Scan(&lastBlock)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return lastBlock, err
}

func (d *DB) UpdateIndexerCursor(ctx context.Context, indexerName string, lastBlock int64) error {
	_, err := d.conn.ExecContext(ctx, `
		INSERT INTO eigenda.indexer_cursors (indexer_name, last_block, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (indexer_name) DO UPDATE SET last_block = $2, updated_at = NOW()`,
		indexerName, lastBlock,
	)
	return err
}

// GetBlobCommitment returns the commitment coordinates for a blob key.
func (d *DB) GetBlobCommitment(ctx context.Context, blobKey string) (commitmentX, commitmentY string, err error) {
	err = d.conn.QueryRowContext(ctx, `
		SELECT commitment_x, commitment_y FROM eigenda.observed_blobs WHERE blob_key = $1`,
		blobKey,
	).Scan(&commitmentX, &commitmentY)
	return
}

// Conn returns the underlying *sql.DB for use by the API server.
func (d *DB) Conn() *sql.DB {
	return d.conn
}

func (d *DB) Close() error {
	return d.conn.Close()
}

CREATE SCHEMA IF NOT EXISTS eigenda;

-- Reference tables
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

-- Time-series tables (hypertables)
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
SELECT create_hypertable('eigenda.retrieval_probes', 'probe_time', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_rp_blob_key ON eigenda.retrieval_probes(blob_key);
CREATE INDEX IF NOT EXISTS idx_rp_age ON eigenda.retrieval_probes(blob_age_hours);

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
SELECT create_hypertable('eigenda.operator_probes', 'probe_time', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_op_blob ON eigenda.operator_probes(blob_key);
CREATE INDEX IF NOT EXISTS idx_op_ts ON eigenda.operator_probes(probe_time);

CREATE TABLE IF NOT EXISTS eigenda.attestation_snapshots (
    snapshot_time            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    blob_key                 VARCHAR(128) NOT NULL,
    quorum_number            INTEGER,
    total_nonsigners         INTEGER,
    signing_stake_percentage FLOAT
);
SELECT create_hypertable('eigenda.attestation_snapshots', 'snapshot_time', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS eigenda.stake_snapshots (
    snapshot_time    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    quorum_id        INTEGER NOT NULL,
    total_stake      NUMERIC,
    operator_count   INTEGER,
    hhi              FLOAT
);
SELECT create_hypertable('eigenda.stake_snapshots', 'snapshot_time', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS eigenda.stake_snapshot_operators (
    snapshot_time    TIMESTAMPTZ NOT NULL,
    quorum_id        INTEGER NOT NULL,
    operator_id      VARCHAR(128),
    stake            NUMERIC,
    stake_pct        FLOAT
);
SELECT create_hypertable('eigenda.stake_snapshot_operators', 'snapshot_time', if_not_exists => TRUE);

CREATE TABLE IF NOT EXISTS eigenda.operator_status_snapshots (
    snapshot_time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    operator_address    VARCHAR(42) NOT NULL,
    metadata_name       TEXT,
    status              VARCHAR(16) NOT NULL,
    total_stakers       INTEGER,
    total_avs           INTEGER,
    tvl_eth             FLOAT
);
SELECT create_hypertable('eigenda.operator_status_snapshots', 'snapshot_time', if_not_exists => TRUE);
CREATE INDEX IF NOT EXISTS idx_oss_addr ON eigenda.operator_status_snapshots(operator_address);
CREATE INDEX IF NOT EXISTS idx_oss_status ON eigenda.operator_status_snapshots(status);

CREATE TABLE IF NOT EXISTS eigenda.ejection_events (
    event_time       TIMESTAMPTZ NOT NULL,
    block_number     BIGINT NOT NULL,
    tx_hash          VARCHAR(66),
    log_index        INTEGER,
    operator_id      VARCHAR(128),
    quorum_number    INTEGER,
    UNIQUE(tx_hash, log_index)
);
SELECT create_hypertable('eigenda.ejection_events', 'event_time', if_not_exists => TRUE);

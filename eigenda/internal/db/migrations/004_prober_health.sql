-- Prober infrastructure health time series.
-- Records DB pool, runtime stats, and worker liveness every minute.

CREATE TABLE IF NOT EXISTS eigenda.prober_health (
    ts                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    goroutines          INT,
    heap_alloc_mb       FLOAT,
    heap_sys_mb         FLOAT,
    db_open_conns       INT,
    db_in_use           INT,
    db_idle             INT,
    db_wait_count       BIGINT,
    db_wait_duration_ms BIGINT,
    uptime_seconds      BIGINT
);
SELECT create_hypertable('eigenda.prober_health', 'ts', if_not_exists => TRUE);

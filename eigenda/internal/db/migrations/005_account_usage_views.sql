-- Account-level usage aggregation views.
-- Analogous to DABEAT's namespace_observations + monitored_namespaces pattern.

-- 1. Per-account summary (all-time)
CREATE OR REPLACE VIEW eigenda.account_usage AS
SELECT
    account_id,
    COUNT(*)                                          AS blob_count,
    SUM(blob_size_bytes)                              AS total_bytes,
    MIN(first_observed_at)                            AS first_seen,
    MAX(first_observed_at)                            AS last_seen,
    COUNT(*) FILTER (WHERE is_self_dispersed)          AS self_dispersed_count,
    AVG(blob_size_bytes)::INT                          AS avg_blob_size,
    MAX(cumulative_payment)                            AS latest_cumulative_payment
FROM eigenda.observed_blobs
WHERE account_id IS NOT NULL AND account_id != ''
GROUP BY account_id;

-- 2. Hourly time series per account (for Grafana time-series panels)
CREATE OR REPLACE VIEW eigenda.account_usage_hourly AS
SELECT
    date_trunc('hour', first_observed_at) AS hour,
    account_id,
    COUNT(*)                              AS blob_count,
    SUM(blob_size_bytes)                  AS total_bytes,
    AVG(blob_size_bytes)::INT             AS avg_blob_size
FROM eigenda.observed_blobs
WHERE account_id IS NOT NULL AND account_id != ''
GROUP BY 1, 2;

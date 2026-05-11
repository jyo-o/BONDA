-- DA layer metadata: claim vs measured comparison.
-- Used by cross-DA comparison dashboard cards.

CREATE TABLE IF NOT EXISTS eigenda.da_layer_metadata (
    da_layer                TEXT PRIMARY KEY,
    chain_decimals          INT,
    native_token            TEXT,
    block_time_seconds      NUMERIC,
    retention_policy        TEXT,        -- 'hard' / 'soft' / 'none'
    retention_days_claim    NUMERIC,     -- official policy days (NULL = no guarantee)
    retention_source_url    TEXT,
    finality_seconds_claim  NUMERIC,
    notes                   TEXT,
    updated_at              TIMESTAMPTZ DEFAULT NOW()
);

INSERT INTO eigenda.da_layer_metadata (
    da_layer, chain_decimals, native_token,
    block_time_seconds, retention_policy, retention_days_claim,
    retention_source_url, finality_seconds_claim, notes
) VALUES
    ('eigenda', 18, 'ETH',
     12, 'hard', 14,
     'https://docs.eigenlayer.xyz/eigenda/overview',
     720,
     'Documented 14-day retention via operator stake commitment. Reverifier age buckets: 5m-15d.'),
    ('ethereum', 18, 'ETH',
     12, 'hard', 18,
     'https://eips.ethereum.org/EIPS/eip-4844',
     780,
     'EIP-4844 blob retention 4096 epochs ~ 18.2 days. Hard cliff after pruning window.'),
    ('celestia', 6, 'TIA',
     6, 'soft', NULL,
     'https://docs.celestia.org/concepts/data-availability-faq',
     6,
     'No hard retention guarantee. Pruning window commonly ~30 days. Light node DAS-based availability.'),
    ('avail', 18, 'AVAIL',
     20, 'soft', NULL,
     'https://docs.availproject.org/docs/learn-about-avail/lcs-and-clients',
     20,
     'Light client retention depends on config. Full archive nodes retain indefinitely. Confidence-based availability.')
ON CONFLICT (da_layer) DO UPDATE SET
    chain_decimals = EXCLUDED.chain_decimals,
    native_token = EXCLUDED.native_token,
    block_time_seconds = EXCLUDED.block_time_seconds,
    retention_policy = EXCLUDED.retention_policy,
    retention_days_claim = EXCLUDED.retention_days_claim,
    retention_source_url = EXCLUDED.retention_source_url,
    finality_seconds_claim = EXCLUDED.finality_seconds_claim,
    notes = EXCLUDED.notes,
    updated_at = NOW();

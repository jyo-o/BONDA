-- Track dispersal cost for self-probed blobs.
-- cumulative_payment from DataAPI PaymentMetadata.

ALTER TABLE eigenda.observed_blobs
    ADD COLUMN IF NOT EXISTS cumulative_payment NUMERIC;

-- ============================================================================
-- bronze.orders — LIVE INCREMENTAL stream
-- ----------------------------------------------------------------------------
-- This pipeline is split into two SELECTs:
--   • backfill — only emits on the FIRST run (no watermark yet). It seeds
--     5000 orders spread evenly across the past 60 days, so silver/gold
--     have something to aggregate immediately.
--   • live     — emits on EVERY run. Generates 4 fresh orders whose
--     timestamps land in the last 25 seconds. Order ids are epoch-based,
--     so successive runs never collide.
--
-- The runner stores max(ordered_at) as the watermark; the incremental
-- filter at the bottom passes only the genuinely-new rows on each
-- subsequent run. With a 30s schedule, that means ~4 orders per 30s in
-- steady state.
-- ============================================================================
-- @merge_strategy: incremental
-- @unique_key: order_id
-- @watermark_column: ordered_at
-- @description: Live orders stream — 60-day backfill on first run, then continuous 30s ingestion. ordered_at is a TIMESTAMP so the "last hour / last 5 minutes" demos work.

WITH backfill AS (
  {% if not is_incremental() %}
    SELECT
      s.id AS order_id,
      1 + ((s.id * 7) % 500) AS customer_id,
      1 + ((s.id * 13) % 50) AS product_id,
      1 + ((s.id * 5) % 5) AS quantity,
      -- 5000 orders spread evenly across the last 60 days. id=5000 lands
      -- ~5 minutes ago (so the live tail doesn't overlap with the seed).
      CURRENT_TIMESTAMP - INTERVAL ((300 + (5000 - s.id) * (60 * 24 * 60 - 5) / 5000)::INTEGER || ' minutes') AS ordered_at,
      CASE (s.id * 11) % 100
        WHEN 0 THEN 'cancelled' WHEN 1 THEN 'cancelled'
        ELSE 'completed'
      END AS status
    FROM generate_series(1, 5000) AS s(id)
  {% else %}
    -- Subsequent runs: emit no backfill rows. The WHERE FALSE is the
    -- typed-NULL trick that keeps the column types stable for UNION.
    SELECT
      NULL::BIGINT AS order_id, NULL::BIGINT AS customer_id, NULL::BIGINT AS product_id,
      NULL::BIGINT AS quantity, NULL::TIMESTAMP AS ordered_at, NULL::VARCHAR AS status
    WHERE FALSE
  {% endif %}
),
live AS (
  -- Every run: 4 brand-new orders with timestamps in the last 25 seconds.
  -- The 10000000 + epoch*10 prefix on order_id guarantees no collision
  -- across runs (or with the backfill range).
  SELECT
    10000000 + CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT) * 10 + s.id AS order_id,
    1 + ((CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT) + s.id * 17) % 500) AS customer_id,
    1 + ((CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT) + s.id * 13) % 50) AS product_id,
    1 + ((CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT) + s.id * 5) % 5) AS quantity,
    CURRENT_TIMESTAMP - INTERVAL ((s.id * 5)::INTEGER || ' seconds') AS ordered_at,
    CASE (CAST(EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) AS BIGINT) + s.id * 11) % 100
      WHEN 0 THEN 'cancelled' WHEN 1 THEN 'cancelled'
      ELSE 'completed'
    END AS status
  FROM generate_series(1, 4) AS s(id)
)
SELECT * FROM backfill
UNION ALL
SELECT * FROM live
{% if is_incremental() %}
WHERE ordered_at > CAST('{{ watermark_value }}' AS TIMESTAMP)
{% endif %}

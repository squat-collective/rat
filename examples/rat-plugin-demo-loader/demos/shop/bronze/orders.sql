-- ============================================================================
-- bronze.orders — INCREMENTAL merge strategy
-- ----------------------------------------------------------------------------
-- The annotations below tell the runner:
--   • merge_strategy:   incremental — append only the new rows each run
--   • unique_key:       order_id    — de-dupe key on re-insert
--   • watermark_column: ordered_at  — column used to filter the delta
--
-- The Jinja `is_incremental()` helper returns false on the first run (no
-- prior watermark) and true thereafter; `{{ watermark_value }}` then holds
-- the previous run's max(ordered_at). The two together let one SQL file
-- serve both the initial backfill and every incremental refresh.
-- ============================================================================
-- @merge_strategy: incremental
-- @unique_key: order_id
-- @watermark_column: ordered_at
-- @description: 5000 orders linking customers to products — appended incrementally.

WITH base AS (
  SELECT
    id AS order_id,
    1 + ((id * 7) % 500) AS customer_id,
    1 + ((id * 13) % 50) AS product_id,
    1 + ((id * 5) % 5) AS quantity,
    DATE '2024-01-01' + (((id * 23) % 540))::INTEGER AS ordered_at,
    CASE (id * 11) % 100
      WHEN 0 THEN 'cancelled' WHEN 1 THEN 'cancelled'
      ELSE 'completed'
    END AS status
  FROM generate_series(1, 5000) AS s(id)
)
SELECT * FROM base
{% if is_incremental() %}
WHERE ordered_at > CAST('{{ watermark_value }}' AS DATE)
{% endif %}

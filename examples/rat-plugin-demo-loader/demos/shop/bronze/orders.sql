-- @merge_strategy: full_refresh
-- @description: 5000 orders linking customers to products.

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

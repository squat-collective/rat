-- @merge_strategy: full_refresh
-- @description: 500 synthetic customers.

WITH base AS (
  SELECT
    id AS customer_id,
    'customer' || id || '@example.com' AS email,
    'Customer ' || id AS name,
    CASE (id * 7) % 6
      WHEN 0 THEN 'FR' WHEN 1 THEN 'DE' WHEN 2 THEN 'NL'
      WHEN 3 THEN 'BE' WHEN 4 THEN 'IT' ELSE 'ES'
    END AS country,
    DATE '2022-01-01' + (((id * 11) % 1095))::INTEGER AS signup_at,
    (id * 17) % 100 < 22 AS is_premium
  FROM generate_series(1, 500) AS s(id)
)
SELECT * FROM base

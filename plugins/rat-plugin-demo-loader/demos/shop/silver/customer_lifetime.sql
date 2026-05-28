-- @merge_strategy: full_refresh
-- @description: Per-customer aggregate — order count, lifetime value, last order date.

SELECT
  customer_id,
  email,
  country,
  is_premium,
  COUNT(*) AS orders,
  ROUND(SUM(revenue_eur), 2) AS lifetime_value_eur,
  MAX(ordered_at) AS last_order_at
FROM {{ ref('silver.orders_enriched') }}
GROUP BY customer_id, email, country, is_premium

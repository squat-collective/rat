-- @merge_strategy: full_refresh
-- @description: Monthly revenue and order count — the time-series gold view.

SELECT
  DATE_TRUNC('month', ordered_at) AS month,
  COUNT(*) AS orders,
  ROUND(SUM(revenue_eur), 2) AS revenue_eur,
  ROUND(AVG(revenue_eur), 2) AS avg_order_value_eur
FROM {{ ref('silver.orders_enriched') }}
GROUP BY 1
ORDER BY 1

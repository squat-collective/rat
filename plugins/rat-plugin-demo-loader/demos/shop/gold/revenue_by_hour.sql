-- ============================================================================
-- gold.revenue_by_hour — hourly trend for the last 48 hours
-- ----------------------------------------------------------------------------
-- Designed for "what's happening right now" dashboards and chat questions
-- like "how much revenue did we make in the last hour?". Refreshed every
-- minute by the scheduler so the picture stays current with the live
-- bronze.orders stream.
-- ============================================================================
-- @merge_strategy: full_refresh
-- @description: Revenue and order count bucketed by hour for the trailing 48 hours — the live counterpart to revenue_by_month.

SELECT
  DATE_TRUNC('hour', ordered_at) AS hour,
  COUNT(*) AS orders,
  ROUND(SUM(revenue_eur), 2) AS revenue_eur,
  ROUND(AVG(revenue_eur), 2) AS avg_order_value_eur
FROM {{ ref('silver.orders_enriched') }}
WHERE ordered_at >= CURRENT_TIMESTAMP - INTERVAL '48 hours'
GROUP BY 1
ORDER BY 1 DESC

-- @merge_strategy: full_refresh
-- @description: Orders joined with customer + product info — the silver fact.

SELECT
  o.order_id,
  o.ordered_at,
  o.status,
  o.quantity,
  c.customer_id,
  c.email,
  c.country,
  c.is_premium,
  p.product_id,
  p.sku,
  p.product_name,
  p.category,
  p.price_eur,
  ROUND(o.quantity * p.price_eur, 2) AS revenue_eur
FROM {{ ref('bronze.orders') }}    AS o
LEFT JOIN {{ ref('bronze.customers') }} AS c USING (customer_id)
LEFT JOIN {{ ref('bronze.products') }}  AS p USING (product_id)
WHERE o.status = 'completed'

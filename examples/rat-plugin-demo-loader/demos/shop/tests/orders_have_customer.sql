-- @severity: error
-- @description: Every order in the silver fact must have a matching customer.

SELECT order_id
FROM {{ ref('silver.orders_enriched') }}
WHERE customer_id IS NULL

-- ============================================================================
-- Quality test: every silver order has a matching customer (FK integrity).
-- ============================================================================
-- @severity: error
-- @description: Every order in the silver fact must have a matching customer_id.
-- @tags: referential_integrity, completeness
-- @remediation: A NULL customer_id means the join in silver.orders_enriched lost the customer (deleted upstream?). Inspect the FK and the silver JOIN.

SELECT order_id
FROM {{ ref('silver.orders_enriched') }}
WHERE customer_id IS NULL

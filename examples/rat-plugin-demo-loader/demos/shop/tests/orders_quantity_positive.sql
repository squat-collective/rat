-- ============================================================================
-- Quality test: bronze.orders.quantity > 0
-- ============================================================================
-- @severity: error
-- @description: Order quantities must be > 0; a non-positive quantity makes downstream revenue calculations meaningless.
-- @tags: validity, range_check, financial
-- @remediation: Look for upstream nullability or a negative-quantity bug in the source system. Rows here are unsafe to aggregate.

SELECT *
FROM {{ ref('bronze.orders') }}
WHERE quantity IS NULL OR quantity <= 0

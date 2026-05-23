-- @severity: error
-- @description: Order quantities must be positive.

SELECT *
FROM {{ ref('bronze.orders') }}
WHERE quantity IS NULL OR quantity <= 0

-- @merge_strategy: full_refresh
-- @description: Top 20 customers by lifetime value.

SELECT *
FROM {{ ref('silver.customer_lifetime') }}
ORDER BY lifetime_value_eur DESC
LIMIT 20

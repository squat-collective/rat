-- @severity: error
-- @description: Every mission must have a mission_id.

SELECT *
FROM {{ ref('bronze.missions') }}
WHERE mission_id IS NULL

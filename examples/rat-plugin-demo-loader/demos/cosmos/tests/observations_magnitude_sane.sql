-- @severity: warn
-- @description: Magnitudes outside -10..30 are suspect (we synthesise within that range).

SELECT *
FROM {{ ref('bronze.observations') }}
WHERE magnitude < -10 OR magnitude > 30

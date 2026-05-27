-- @merge_strategy: full_refresh
-- @description: Satellites grouped by type and operator — fleet summary for the dashboard.

SELECT
  satellite_type,
  operator,
  COUNT(*) AS satellites,
  ROUND(AVG(orbit_km), 0) AS avg_orbit_km
FROM {{ ref('bronze.satellites') }}
GROUP BY satellite_type, operator
ORDER BY satellites DESC

-- @merge_strategy: full_refresh
-- @description: One row per day-planet — observation counts and brightness extremes.

SELECT
  CAST(observed_at AS DATE) AS observation_date,
  target_planet,
  COUNT(*) AS observation_count,
  ROUND(MIN(magnitude), 2) AS brightest_mag,
  ROUND(MAX(magnitude), 2) AS dimmest_mag,
  ROUND(AVG(magnitude), 2) AS avg_mag
FROM {{ ref('bronze.observations') }}
GROUP BY 1, 2

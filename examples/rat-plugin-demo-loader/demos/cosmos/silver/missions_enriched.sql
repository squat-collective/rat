-- @merge_strategy: full_refresh
-- @description: Adds derived columns (mission_type, decade) to the bronze missions.

SELECT
  m.mission_id,
  m.mission_name,
  m.program,
  m.target_planet,
  CASE m.target_planet WHEN 'Moon' THEN 'lunar' ELSE 'planetary' END AS mission_type,
  m.launched_at,
  EXTRACT(YEAR FROM m.launched_at) AS launched_year,
  (EXTRACT(YEAR FROM m.launched_at) - EXTRACT(YEAR FROM m.launched_at) % 10) AS decade,
  m.status,
  (m.status = 'success') AS succeeded,
  m.payload_kg
FROM {{ ref('bronze.missions') }} AS m

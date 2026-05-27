-- @merge_strategy: full_refresh
-- @description: 120 satellites by type and operator.

WITH base AS (
  SELECT
    id AS satellite_id,
    'SAT-' || lpad(id::VARCHAR, 4, '0') AS satellite_name,
    CASE (id * 7) % 4
      WHEN 0 THEN 'communications' WHEN 1 THEN 'observation'
      WHEN 2 THEN 'navigation'     ELSE 'scientific'
    END AS satellite_type,
    CASE (id * 23) % 5
      WHEN 0 THEN 'SpaceX'    WHEN 1 THEN 'NASA' WHEN 2 THEN 'ESA'
      WHEN 3 THEN 'Eutelsat'  ELSE 'Iridium'
    END AS operator,
    DATE '2015-01-01' + (((id * 29) % 3650))::INTEGER AS launched_at,
    400 + ((id * 17) % 35000) AS orbit_km
  FROM generate_series(1, 120) AS s(id)
)
SELECT * FROM base

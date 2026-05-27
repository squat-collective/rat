-- @merge_strategy: full_refresh
-- @description: 200 synthetic rocket missions across six agencies.

WITH base AS (
  SELECT
    id,
    'M-' || lpad(id::VARCHAR, 4, '0') AS mission_id,
    'Mission ' || id AS mission_name,
    CASE (id * 31) % 6
      WHEN 0 THEN 'SpaceX'  WHEN 1 THEN 'NASA'    WHEN 2 THEN 'ESA'
      WHEN 3 THEN 'Roscosmos' WHEN 4 THEN 'JAXA'  ELSE 'ISRO'
    END AS program,
    CASE (id * 17) % 5
      WHEN 0 THEN 'Moon'   WHEN 1 THEN 'Mars'  WHEN 2 THEN 'Venus'
      WHEN 3 THEN 'Jupiter' ELSE 'Saturn'
    END AS target_planet,
    DATE '2020-01-01' + (((id * 11) % 2200))::INTEGER AS launched_at,
    CASE
      WHEN (id * 13) % 100 < 78 THEN 'success'
      WHEN (id * 13) % 100 < 92 THEN 'partial'
      ELSE 'failure'
    END AS status,
    1000 + ((id * 37) % 9000) AS payload_kg
  FROM generate_series(1, 200) AS s(id)
)
SELECT * FROM base

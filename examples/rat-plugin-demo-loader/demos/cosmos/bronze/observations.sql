-- @merge_strategy: full_refresh
-- @description: 1000 synthetic telescope observations: when, where, magnitude.

WITH base AS (
  SELECT
    id,
    CASE (id * 7) % 4
      WHEN 0 THEN 'Mauna Kea' WHEN 1 THEN 'Paranal'
      WHEN 2 THEN 'Hubble'    ELSE 'JWST'
    END AS observatory,
    CASE (id * 5) % 5
      WHEN 0 THEN 'Moon'   WHEN 1 THEN 'Mars'  WHEN 2 THEN 'Venus'
      WHEN 3 THEN 'Jupiter' ELSE 'Saturn'
    END AS target_planet,
    (TIMESTAMP '2025-01-01 00:00:00' + INTERVAL ((id * 91) % 31536000) SECOND) AS observed_at,
    ROUND(-2 + ((id * 41) % 2000) / 100.0, 2) AS magnitude,
    CASE (id * 19) % 3
      WHEN 0 THEN 'spectroscopy' WHEN 1 THEN 'photometry'
      ELSE 'astrometry'
    END AS instrument
  FROM generate_series(1, 1000) AS s(id)
)
SELECT * FROM base

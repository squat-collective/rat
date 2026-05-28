-- ============================================================================
-- bronze.observations — APPEND_ONLY merge strategy
-- ----------------------------------------------------------------------------
-- Every run extends the table; nothing is overwritten. Good fit for log-like
-- data where history matters. The runner appends every row this query
-- produces, so rerunning grows the table by another batch (we use
-- generate_series for the demo — in real life this would be a CDC stream
-- or a daily file).
-- ============================================================================
-- @merge_strategy: append_only
-- @description: Telescope observations — append-only feed.

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

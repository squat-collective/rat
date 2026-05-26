-- ============================================================================
-- bronze.gigs — APPEND_ONLY merge strategy
-- ----------------------------------------------------------------------------
-- A gig is an immutable event in time — once it happened, it doesn't change.
-- append_only models that: every run extends the table; the runner never
-- rewrites existing rows.
-- ============================================================================
-- @merge_strategy: append_only
-- @description: Gig schedule across the underground venues — append-only history.

WITH base AS (
  SELECT
    id AS gig_id,
    1 + ((id * 7) % 50) AS venue_id,
    DATE '2023-01-01' + (((id * 13) % 900))::INTEGER AS gig_date,
    CASE (id * 23) % 12
      WHEN 0 THEN 'Cassettes Volees'   WHEN 1 THEN 'Béton Tendre'
      WHEN 2 THEN 'Marie & The Hounds' WHEN 3 THEN 'Neon Saxons'
      WHEN 4 THEN 'Solar Squat'        WHEN 5 THEN 'Lichen Stereo'
      WHEN 6 THEN 'Bête Noire'         WHEN 7 THEN 'Ultragramme'
      WHEN 8 THEN 'Verre Brut'         WHEN 9 THEN 'Nuit Furtive'
      WHEN 10 THEN 'Saint Frequency'   ELSE 'Cobalt Garage'
    END AS artist,
    CASE (id * 19) % 6
      WHEN 0 THEN 'post-punk'   WHEN 1 THEN 'techno'  WHEN 2 THEN 'noise'
      WHEN 3 THEN 'shoegaze'    WHEN 4 THEN 'hardcore' ELSE 'dub'
    END AS genre,
    ((id * 29) % 100 < 28) AS sold_out
  FROM generate_series(1, 500) AS s(id)
)
SELECT * FROM base

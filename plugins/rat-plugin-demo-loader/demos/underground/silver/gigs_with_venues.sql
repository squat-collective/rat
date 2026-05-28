-- @merge_strategy: full_refresh
-- @description: Gigs joined to their venue — the silver fact.

SELECT
  g.gig_id,
  g.gig_date,
  g.artist,
  g.genre,
  g.sold_out,
  v.venue_id,
  v.venue_name,
  v.city,
  v.venue_type,
  v.capacity
FROM {{ ref('bronze.gigs') }} AS g
LEFT JOIN {{ ref('bronze.venues') }} AS v USING (venue_id)

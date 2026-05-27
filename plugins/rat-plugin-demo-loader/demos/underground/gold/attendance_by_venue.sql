-- @merge_strategy: full_refresh
-- @description: Per-venue lifetime attendance — heatmap of the scene.

SELECT
  g.venue_id,
  g.venue_name,
  g.city,
  g.venue_type,
  COUNT(DISTINCT g.gig_id) AS gigs,
  SUM(a.attendees) AS lifetime_attendance,
  SUM(a.paid_attendees) AS paid_attendance,
  ROUND(SUM(a.total_revenue_eur), 2) AS lifetime_revenue_eur
FROM {{ ref('silver.gigs_with_venues') }} AS g
LEFT JOIN {{ ref('silver.attendance_per_gig') }} AS a USING (gig_id)
GROUP BY g.venue_id, g.venue_name, g.city, g.venue_type
ORDER BY lifetime_attendance DESC

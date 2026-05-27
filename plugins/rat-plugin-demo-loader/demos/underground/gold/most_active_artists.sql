-- @merge_strategy: full_refresh
-- @description: Artists ranked by total gigs and pulled attendance.

SELECT
  g.artist,
  COUNT(*) AS gigs,
  SUM(a.attendees) AS total_attendance,
  ROUND(AVG(a.attendees), 1) AS avg_per_gig,
  ROUND(SUM(a.total_revenue_eur), 2) AS revenue_eur
FROM {{ ref('silver.gigs_with_venues') }} AS g
LEFT JOIN {{ ref('silver.attendance_per_gig') }} AS a USING (gig_id)
GROUP BY g.artist
ORDER BY total_attendance DESC

-- @merge_strategy: full_refresh
-- @description: One row per gig with total and paid attendee counts.

SELECT
  gig_id,
  COUNT(*) AS attendees,
  SUM(CASE WHEN paid THEN 1 ELSE 0 END) AS paid_attendees,
  ROUND(SUM(price_eur), 2) AS total_revenue_eur
FROM {{ ref('bronze.attendees') }}
GROUP BY gig_id

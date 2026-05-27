-- @merge_strategy: full_refresh
-- @description: 5000 attendance records — who came, did they pay.

WITH base AS (
  SELECT
    id AS attendance_id,
    1 + ((id * 7) % 500) AS gig_id,
    100 + ((id * 41) % 2000) AS person_id,
    ((id * 17) % 100 < 75) AS paid,
    ROUND(CASE WHEN (id * 17) % 100 < 75 THEN 5 + ((id * 23) % 20) ELSE 0 END, 2) AS price_eur
  FROM generate_series(1, 5000) AS s(id)
)
SELECT * FROM base

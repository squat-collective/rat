-- @severity: error
-- @description: Attendance counts can never be negative.

SELECT *
FROM {{ ref('silver.attendance_per_gig') }}
WHERE attendees < 0 OR paid_attendees < 0

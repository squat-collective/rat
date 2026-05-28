-- ============================================================================
-- Quality test: attendance counts are non-negative (sanity guard).
-- ============================================================================
-- @severity: error
-- @description: Attendance counts must be >= 0.
-- @tags: validity, range_check
-- @remediation: A negative count means the silver aggregation overflowed or the bronze attendees table contains duplicates being subtracted. Inspect the silver SQL.

SELECT *
FROM {{ ref('silver.attendance_per_gig') }}
WHERE attendees < 0 OR paid_attendees < 0

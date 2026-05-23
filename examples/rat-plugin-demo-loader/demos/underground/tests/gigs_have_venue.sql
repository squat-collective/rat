-- @severity: error
-- @description: Every gig must reference a venue_id.

SELECT *
FROM {{ ref('bronze.gigs') }}
WHERE venue_id IS NULL

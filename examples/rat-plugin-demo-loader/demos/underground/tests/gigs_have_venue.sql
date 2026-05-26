-- ============================================================================
-- Quality test: every gig points at a venue (foreign-key completeness).
-- ============================================================================
-- @severity: error
-- @description: Every gig must reference a venue_id — referential integrity.
-- @tags: referential_integrity, completeness
-- @remediation: Drop or backfill orphan gigs upstream; broken FKs propagate to silver joins.

SELECT *
FROM {{ ref('bronze.gigs') }}
WHERE venue_id IS NULL

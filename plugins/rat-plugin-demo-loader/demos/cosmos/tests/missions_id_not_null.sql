-- ============================================================================
-- Quality test: bronze.missions.mission_id IS NOT NULL
-- ----------------------------------------------------------------------------
-- A row returned = a violation. severity=error means a failed test fails
-- the next run that publishes the table. Tags + remediation are displayed
-- on the Quality tab and on test results.
-- ============================================================================
-- @severity: error
-- @description: Every mission must have a mission_id — primary-key completeness.
-- @tags: completeness, primary_key
-- @remediation: Investigate the source: bronze.missions.mission_id should be a non-null synthetic key. Re-run if upstream input was incomplete.

SELECT *
FROM {{ ref('bronze.missions') }}
WHERE mission_id IS NULL

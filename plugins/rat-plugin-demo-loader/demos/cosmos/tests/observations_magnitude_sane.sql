-- ============================================================================
-- Quality test: bronze.observations.magnitude is in a sane astronomical range
-- ----------------------------------------------------------------------------
-- severity=warn means the run continues even if violations exist — useful for
-- soft checks where you want a heads-up but not a hard fail.
-- ============================================================================
-- @severity: warn
-- @description: Apparent magnitude outside -10..30 is physically implausible.
-- @tags: validity, range_check
-- @remediation: Suspect instrument calibration or a unit-conversion bug in the bronze ingest.

SELECT *
FROM {{ ref('bronze.observations') }}
WHERE magnitude < -10 OR magnitude > 30

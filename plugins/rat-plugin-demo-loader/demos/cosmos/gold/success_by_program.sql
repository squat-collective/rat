-- @merge_strategy: full_refresh
-- @description: Mission success rate per agency — the bottom-line metric for each space program.

SELECT
  program,
  COUNT(*) AS missions,
  SUM(CASE WHEN succeeded THEN 1 ELSE 0 END) AS successes,
  SUM(CASE WHEN status = 'failure' THEN 1 ELSE 0 END) AS failures,
  ROUND(100.0 * SUM(CASE WHEN succeeded THEN 1 ELSE 0 END) / COUNT(*), 1) AS success_pct
FROM {{ ref('silver.missions_enriched') }}
GROUP BY program
ORDER BY success_pct DESC

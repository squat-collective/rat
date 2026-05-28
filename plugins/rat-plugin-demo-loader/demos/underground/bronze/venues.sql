-- @merge_strategy: full_refresh
-- @description: 50 DIY venues across European cities.

WITH base AS (
  SELECT
    id AS venue_id,
    CASE (id * 7) % 10
      WHEN 0 THEN 'Squat de la Lune'  WHEN 1 THEN 'Bunker 23'
      WHEN 2 THEN 'Cellier Rouge'     WHEN 3 THEN 'La Friche'
      WHEN 4 THEN 'Atelier 9'         WHEN 5 THEN 'Garage Noir'
      WHEN 6 THEN 'Le Hangar'         WHEN 7 THEN 'Cave Brutale'
      WHEN 8 THEN 'Usine Z'           ELSE 'Quartier Libre'
    END || ' #' || id AS venue_name,
    CASE (id * 13) % 6
      WHEN 0 THEN 'Paris'   WHEN 1 THEN 'Berlin' WHEN 2 THEN 'Marseille'
      WHEN 3 THEN 'Lyon'    WHEN 4 THEN 'Brussels' ELSE 'Amsterdam'
    END AS city,
    CASE (id * 5) % 4
      WHEN 0 THEN 'squat'   WHEN 1 THEN 'warehouse'
      WHEN 2 THEN 'basement' ELSE 'collective'
    END AS venue_type,
    50 + ((id * 17) % 350) AS capacity,
    2005 + ((id * 11) % 20) AS opened_year
  FROM generate_series(1, 50) AS s(id)
)
SELECT * FROM base

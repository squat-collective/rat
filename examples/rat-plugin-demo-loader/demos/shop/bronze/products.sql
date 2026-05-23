-- @merge_strategy: full_refresh
-- @description: 50 products across five categories.

WITH base AS (
  SELECT
    id AS product_id,
    'PROD-' || lpad(id::VARCHAR, 3, '0') AS sku,
    CASE (id * 13) % 5
      WHEN 0 THEN 'Cassette ' || id  WHEN 1 THEN 'Sticker pack ' || id
      WHEN 2 THEN 'T-shirt #'    || id WHEN 3 THEN 'Tote bag #' || id
      ELSE 'Zine vol.' || id
    END AS product_name,
    CASE (id * 13) % 5
      WHEN 0 THEN 'audio'    WHEN 1 THEN 'stickers'
      WHEN 2 THEN 'apparel'  WHEN 3 THEN 'accessories' ELSE 'print'
    END AS category,
    ROUND(5 + ((id * 41) % 4500) / 100.0, 2) AS price_eur
  FROM generate_series(1, 50) AS s(id)
)
SELECT * FROM base

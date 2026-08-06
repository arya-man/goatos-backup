-- Whole-shed readings: rows where the weighing was recorded against the shed, not
-- an animal. These carry weight but can never carry a gain per animal, so they are
-- kept in their own lane and never enter any per-animal figure.
--
-- 121 rows. Output feeds build_dataset.py as shedlevel.json.
SELECT
  date,
  farm,
  REGEXP_REPLACE(UPPER(TRIM(shed)), r"\s+", " ") AS shed,
  shed_tag,
  gender,
  goat_type,
  ROUND(AVG(CAST(avg_weight_kg AS FLOAT64)), 2) AS avg_kg
FROM `goatos-sheets.weights.weights_db_clean_dev`
WHERE goat_id IS NULL
   OR UPPER(TRIM(goat_id)) IN ('', 'NO TAG', 'NO ID', 'NA', '-', 'NIL', '?')
GROUP BY 1, 2, 3, 4, 5, 6
ORDER BY date

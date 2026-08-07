WITH base AS (
  SELECT goat_id, farm, date,
    REGEXP_REPLACE(UPPER(TRIM(shed)), r'\s+', ' ') shed_norm,
    breed, gender, goat_type, shed_tag,
    AVG(CAST(avg_weight_kg AS FLOAT64)) wt
  FROM `goatos-sheets.weights.weights_db_clean_dev`
  WHERE goat_id IS NOT NULL AND TRIM(goat_id) NOT IN ('','No tag','-','NA')
    AND avg_weight_kg IS NOT NULL
  GROUP BY 1,2,3,4,5,6,7,8
),
n AS (
  SELECT *,
    TRIM(REGEXP_REPLACE(shed_norm, r'[-\s]*PART\s*\d+$','')) shed_base,
    REGEXP_EXTRACT(shed_norm, r'PART\s*(\d+)$') partition_no
  FROM base
),
seq AS (
  SELECT *, ROW_NUMBER() OVER(PARTITION BY goat_id ORDER BY date) rn,
    COUNT(*) OVER(PARTITION BY goat_id) n_weighings,
    FIRST_VALUE(wt) OVER(PARTITION BY goat_id ORDER BY date) first_wt,
    FIRST_VALUE(date) OVER(PARTITION BY goat_id ORDER BY date) first_date,
    LAST_VALUE(wt) OVER(PARTITION BY goat_id ORDER BY date
      ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) last_wt,
    LAST_VALUE(date) OVER(PARTITION BY goat_id ORDER BY date
      ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) last_date,
    LAST_VALUE(shed_base) OVER(PARTITION BY goat_id ORDER BY date
      ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) cur_shed_base,
    LAST_VALUE(partition_no) OVER(PARTITION BY goat_id ORDER BY date
      ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) cur_partition,
    LAST_VALUE(farm) OVER(PARTITION BY goat_id ORDER BY date
      ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) cur_farm,
    LAST_VALUE(shed_tag) OVER(PARTITION BY goat_id ORDER BY date
      ROWS BETWEEN UNBOUNDED PRECEDING AND UNBOUNDED FOLLOWING) cur_tag
  FROM n
)
SELECT goat_id, ANY_VALUE(cur_farm) farm, ANY_VALUE(cur_shed_base) shed,
  ANY_VALUE(cur_partition) part, ANY_VALUE(breed) breed, ANY_VALUE(gender) gender,
  ANY_VALUE(goat_type) goat_type, ANY_VALUE(cur_tag) shed_tag,
  ANY_VALUE(n_weighings) n_weighings,
  ANY_VALUE(first_date) first_date, ROUND(ANY_VALUE(first_wt),2) first_wt,
  ANY_VALUE(last_date) last_date, ROUND(ANY_VALUE(last_wt),2) last_wt,
  DATE_DIFF(ANY_VALUE(last_date), ANY_VALUE(first_date), DAY) span_days,
  ROUND(SAFE_DIVIDE(ANY_VALUE(last_wt)-ANY_VALUE(first_wt),
        NULLIF(DATE_DIFF(ANY_VALUE(last_date),ANY_VALUE(first_date),DAY),0)),4) overall_adg
FROM seq GROUP BY goat_id

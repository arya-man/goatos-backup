WITH w AS (
  SELECT goat_id, ANY_VALUE(farm) farm, ANY_VALUE(shed) shed, ANY_VALUE(shed_tag) shed_tag, ANY_VALUE(breed) breed,
         ANY_VALUE(gender) gender, ANY_VALUE(goat_type) goat_type, date,
         AVG(CAST(avg_weight_kg AS FLOAT64)) wt, COUNT(*) same_day_rows,
         MAX(CAST(avg_weight_kg AS FLOAT64))-MIN(CAST(avg_weight_kg AS FLOAT64)) same_day_spread
  FROM `goatos-sheets.weights.weights_db_clean_dev`
  WHERE goat_id IS NOT NULL AND goat_id NOT IN ('','No tag','-') AND avg_weight_kg IS NOT NULL
  GROUP BY goat_id, date
),
p AS (
  SELECT *,
    LAG(wt) OVER(PARTITION BY goat_id ORDER BY date) pw,
    LAG(date) OVER(PARTITION BY goat_id ORDER BY date) pd,
    LAG(shed) OVER(PARTITION BY goat_id ORDER BY date) pshed,
    LAG(shed_tag) OVER(PARTITION BY goat_id ORDER BY date) ptag,
    LEAD(wt) OVER(PARTITION BY goat_id ORDER BY date) nw,
    LEAD(date) OVER(PARTITION BY goat_id ORDER BY date) nd
  FROM w
),
ev AS (SELECT goat_id, event_type, event_date, shifting_reason, src_shed, dst_shed, birth_date, mother_id
       FROM `goatos-sheets.goatsDB.goat_activity_timeline`),
bd AS (SELECT goat_id, MIN(birth_date) birth_date FROM ev WHERE birth_date IS NOT NULL GROUP BY 1)
SELECT
  p.goat_id, p.farm, p.shed, p.pshed, p.shed_tag, p.ptag, p.breed, p.gender, p.goat_type,
  p.pd AS prev_date, p.pw AS prev_wt, p.date AS cur_date, p.wt AS cur_wt,
  p.nd AS next_date, p.nw AS next_wt,
  p.same_day_rows, p.same_day_spread,
  DATE_DIFF(p.date, p.pd, DAY) days,
  ROUND(p.wt - p.pw, 3) delta,
  ROUND((p.wt - p.pw)/NULLIF(DATE_DIFF(p.date,p.pd,DAY),0), 4) adg,
  bd.birth_date,
  DATE_DIFF(p.date, bd.birth_date, DAY) age_days,
  (SELECT COUNT(*) FROM `goatos-sheets.healthDB.diagnosis_clean_table` h
     WHERE h.Goat_ID = p.goat_id AND h.Date BETWEEN DATE_SUB(p.pd, INTERVAL 7 DAY) AND p.date) health_n,
  (SELECT STRING_AGG(DISTINCT CONCAT(IFNULL(h.Diseases,'?'),'|',IFNULL(h.Symptoms,''),'|',IFNULL(h.Status,''),'|',CAST(h.Date AS STRING)), ' ;; ')
     FROM `goatos-sheets.healthDB.diagnosis_clean_table` h
     WHERE h.Goat_ID = p.goat_id AND h.Date BETWEEN DATE_SUB(p.pd, INTERVAL 7 DAY) AND p.date) health_detail,
  (SELECT COUNT(*) FROM ev e WHERE e.mother_id = p.goat_id AND e.event_type='Birth'
     AND e.event_date BETWEEN DATE_SUB(p.pd, INTERVAL 14 DAY) AND p.date) kidding_n,
  (SELECT MIN(e.event_date) FROM ev e WHERE e.mother_id = p.goat_id AND e.event_type='Birth'
     AND e.event_date BETWEEN DATE_SUB(p.pd, INTERVAL 14 DAY) AND p.date) kidding_date,
  (SELECT COUNT(*) FROM ev e WHERE e.goat_id = p.goat_id AND e.event_type='Abortion'
     AND e.event_date BETWEEN DATE_SUB(p.pd, INTERVAL 14 DAY) AND p.date) abortion_n,
  (SELECT STRING_AGG(DISTINCT CONCAT(e.shifting_reason,'@',CAST(e.event_date AS STRING),':',IFNULL(e.src_shed,''),'>',IFNULL(e.dst_shed,'')), ' ;; ')
     FROM ev e WHERE e.goat_id = p.goat_id AND e.event_type='Shifting'
     AND e.event_date BETWEEN p.pd AND p.date AND e.shifting_reason IS NOT NULL AND e.shifting_reason != '-') shift_detail
FROM p LEFT JOIN bd ON bd.goat_id = p.goat_id
WHERE p.pw IS NOT NULL AND p.date > p.pd AND p.wt > p.pw

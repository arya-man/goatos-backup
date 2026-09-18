-- +goose Up
-- Ask Mesha needs the same operational facts the dashboard cards show: farm-born
-- animals from the current scope and mortality tiles using the current Counts
-- death predicate. Both view rebuilds preserve existing column order and append
-- new columns so CREATE OR REPLACE VIEW is safe on already-migrated databases.

CREATE OR REPLACE VIEW ceo_ai.animal_current_scope AS
SELECT
    g.tenant_id                                   AS tenant_id,
    g.goat_id                                     AS animal_id,
    g.park_id                                     AS park_id,
    pk.name                                       AS park_label,
    g.shed_id                                     AS shed_id,
    sh.name                                       AS shed_label,
    g.species                                     AS species,
    g.management_stage                            AS management_stage,
    g.lifecycle_status                            AS lifecycle_status,
    g.sex                                         AS sex,
    COALESCE(b.canonical_name, g.breed)           AS breed,
    (((now() AT TIME ZONE 'Asia/Kolkata')::date) - COALESCE(g.dob, g.approx_dob))::int
                                                  AS age_days,
    NULLIF(gsp.partition_label, 'whole')           AS partition_label,
    g.origin_type                                 AS origin_type
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id
LEFT JOIN breeds    b  ON b.breed_id     = g.breed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id;

CREATE OR REPLACE VIEW ceo_ai.mortality_base AS
WITH dead AS (
    SELECT g.tenant_id, g.park_id,
           COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                    (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date) AS event_date,
           g.management_stage,
           g.dob,
           g.approx_dob,
           g.goat_id,
           hdc.goat_id IS NOT NULL AS recorded_cause,
           COALESCE(inferred.has_case, false) AS inferred_cause
    FROM goats g
    LEFT JOIN health_death_causes hdc ON hdc.tenant_id = g.tenant_id AND hdc.goat_id = g.goat_id
    LEFT JOIN LATERAL (
        SELECT true AS has_case
        FROM health_cases c
        WHERE c.tenant_id = g.tenant_id
          AND c.goat_id = g.goat_id
          AND c.status IN ('closed_dead', 'held_death_review')
          AND c.start_date <= COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                                       (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date)
          AND (c.closed_at IS NULL OR (c.closed_at AT TIME ZONE 'Asia/Kolkata')::date >= COALESCE((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date,
                                                                                                   (g.updated_at AT TIME ZONE 'Asia/Kolkata')::date))
        LIMIT 1
    ) inferred ON true
    WHERE (g.exited_at IS NOT NULL OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
      AND (g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
),
deaths AS (
    SELECT tenant_id, park_id, event_date,
           COUNT(*)::bigint AS deaths,
           COUNT(*) FILTER (WHERE management_stage ILIKE 'k%' OR ((event_date - COALESCE(dob, approx_dob)) < 365))::bigint AS kid_deaths,
           COUNT(*) FILTER (WHERE NOT (management_stage ILIKE 'k%' OR ((event_date - COALESCE(dob, approx_dob)) < 365)))::bigint AS adult_deaths,
           COUNT(*) FILTER (WHERE ((event_date - COALESCE(dob, approx_dob)) BETWEEN 0 AND 7))::bigint AS first_week_deaths,
           COUNT(*) FILTER (WHERE recorded_cause OR inferred_cause)::bigint AS cause_established
    FROM dead
    GROUP BY tenant_id, park_id, event_date
),
pop AS (
    SELECT tenant_id, park_id, COUNT(*)::bigint AS active_population
    FROM goats
    WHERE lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, park_id
)
SELECT
    d.tenant_id                                   AS tenant_id,
    d.event_date                                  AS event_date,
    pk.name                                       AS park_label,
    d.deaths                                      AS deaths,
    COALESCE(pop.active_population, 0)            AS active_population,
    d.kid_deaths                                  AS kid_deaths,
    d.adult_deaths                                AS adult_deaths,
    d.first_week_deaths                           AS first_week_deaths,
    d.cause_established                           AS cause_established
FROM deaths d
LEFT JOIN locations pk ON pk.location_id = d.park_id
LEFT JOIN pop ON pop.tenant_id = d.tenant_id AND pop.park_id IS NOT DISTINCT FROM d.park_id;

-- +goose Down
CREATE OR REPLACE VIEW ceo_ai.animal_current_scope AS
SELECT
    g.tenant_id                                   AS tenant_id,
    g.goat_id                                     AS animal_id,
    g.park_id                                     AS park_id,
    pk.name                                       AS park_label,
    g.shed_id                                     AS shed_id,
    sh.name                                       AS shed_label,
    g.species                                     AS species,
    g.management_stage                            AS management_stage,
    g.lifecycle_status                            AS lifecycle_status,
    g.sex                                         AS sex,
    COALESCE(b.canonical_name, g.breed)           AS breed,
    (((now() AT TIME ZONE 'Asia/Kolkata')::date) - COALESCE(g.dob, g.approx_dob))::int
                                                  AS age_days,
    NULLIF(gsp.partition_label, 'whole')           AS partition_label
FROM goats g
LEFT JOIN locations pk ON pk.location_id = g.park_id
LEFT JOIN locations sh ON sh.location_id = g.shed_id
LEFT JOIN breeds    b  ON b.breed_id     = g.breed_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = g.tenant_id AND gsp.goat_id = g.goat_id;

CREATE OR REPLACE VIEW ceo_ai.mortality_base AS
WITH deaths AS (
    SELECT g.tenant_id, g.park_id,
           (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
           COUNT(*)::bigint AS deaths
    FROM goats g
    WHERE g.exited_at IS NOT NULL AND g.exit_reason IN ('death','dead','mortality')
    GROUP BY g.tenant_id, g.park_id, (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date
),
pop AS (
    SELECT tenant_id, park_id, COUNT(*)::bigint AS active_population
    FROM goats
    WHERE lifecycle_status NOT IN ('dead','sold','culled','transferred','lost','merged','inactive')
    GROUP BY tenant_id, park_id
)
SELECT
    d.tenant_id                                   AS tenant_id,
    d.event_date                                  AS event_date,
    pk.name                                       AS park_label,
    d.deaths                                      AS deaths,
    COALESCE(pop.active_population, 0)            AS active_population
FROM deaths d
LEFT JOIN locations pk ON pk.location_id = d.park_id
LEFT JOIN pop ON pop.tenant_id = d.tenant_id AND pop.park_id IS NOT DISTINCT FROM d.park_id;

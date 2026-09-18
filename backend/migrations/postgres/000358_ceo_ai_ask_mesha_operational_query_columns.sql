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
WITH deaths AS (
    SELECT g.tenant_id, g.park_id,
           (g.exited_at AT TIME ZONE 'Asia/Kolkata')::date AS event_date,
           COUNT(*)::bigint AS deaths,
           COUNT(*) FILTER (WHERE g.management_stage ILIKE 'k%' OR (((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date - COALESCE(g.dob, g.approx_dob)) < 365))::bigint AS kid_deaths,
           COUNT(*) FILTER (WHERE NOT (g.management_stage ILIKE 'k%' OR (((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date - COALESCE(g.dob, g.approx_dob)) < 365)))::bigint AS adult_deaths,
           COUNT(*) FILTER (WHERE (((g.exited_at AT TIME ZONE 'Asia/Kolkata')::date - COALESCE(g.dob, g.approx_dob)) BETWEEN 0 AND 7))::bigint AS first_week_deaths,
           COUNT(*) FILTER (WHERE hdc.goat_id IS NOT NULL)::bigint AS cause_established
    FROM goats g
    LEFT JOIN health_death_causes hdc ON hdc.tenant_id = g.tenant_id AND hdc.goat_id = g.goat_id
    WHERE g.exited_at IS NOT NULL
      AND (g.exit_reason = 'died' OR (g.exit_reason IS NULL AND g.lifecycle_status = 'dead'))
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

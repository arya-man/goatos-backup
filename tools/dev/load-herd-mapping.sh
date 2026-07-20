#!/usr/bin/env bash
# Apply an RFID -> (stage, shed, breed, species, age band, sex) mapping to the herd.
#
# WHY THIS EXISTS
# Stage, shed tag, breed and kid/adult classification are owned by the Counting DB group mapping,
# which lives outside this repo. A seed leaves those fields empty or stale, so an operator applies
# the roster afterwards. This is the repeatable way to do that in local, staging and production.
#
# SAFETY MODEL
#   * DATABASE_URL must be passed explicitly. No default, so it can never hit the wrong database.
#   * Dry run is the DEFAULT; writing requires --apply.
#   * One transaction. Only live, non-merged animals in the given tenant and park are touched.
#   * ANALYZE runs after apply (stage/breed/shed are all GROUP BY keys on Counts Breakdown).
#
# AMBIGUITY RESOLUTION
# The Counting DB assigns attributes per GROUP, not per animal, so some groups carry several
# candidate sheds ("Godel 1 - Part 1 + Part 2") or breeds ("Anantapur Sheep / Beetal"). Those are
# NOT guessed: if the animal's current value is one of the candidates it is KEPT, which resolves
# most of them from existing state. Only when the current value is outside the candidate list does
# the first candidate win, and every such case is counted in the report as a forced pick so it is
# never silent.
#
# DERIVED FIELDS
#   species  - from the resolved breed, using the mapping doc's own rule: a breed name containing
#              "Sheep" is species 'sheep', otherwise 'goat'. Not taken from the doc's TYPE column,
#              which is "Ambiguous" wherever the breed is.
#   breed_id - looked up from breeds.canonical_name. goats.breed_id is an FK, so updating only the
#              breed text would leave animals pointing at the wrong or a retired breed row.
#
# THE MAPPING FILE IS NOT COMMITTED — it is a roster row dump, and repo hygiene (AGENTS.md) keeps
# those out of git; `*.csv` is already gitignored. tools/dev/data/ is the conventional location.
#
# USAGE
#   DATABASE_URL=postgres://... tools/dev/load-herd-mapping.sh \
#     --file tools/dev/data/cpt-full-mapping.csv \
#     --tenant <uuid> --park CPT
#   ... re-run with --apply once the dry-run report looks right.
#
# FILE FORMAT (CSV, header required)
#   rfid,stage,shed_options,breed_options,age_band,sex
#   shed_options / breed_options are '|'-separated candidate lists.

set -euo pipefail

MAPPING_FILE=""; TENANT_ID=""; PARK_CODE=""; APPLY=0; CREATE_MISSING=0

while [ $# -gt 0 ]; do
  case "$1" in
    --file) MAPPING_FILE="${2:-}"; shift 2 ;;
    --tenant) TENANT_ID="${2:-}"; shift 2 ;;
    --park) PARK_CODE="${2:-}"; shift 2 ;;
    --apply) APPLY=1; shift ;;
    --create-missing) CREATE_MISSING=1; shift ;;
    -h|--help) sed -n '1,45p' "$0"; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

[ -n "${DATABASE_URL:-}" ] || { echo "DATABASE_URL is required (no default, so the wrong database can never be hit by accident)." >&2; exit 2; }
[ -n "$MAPPING_FILE" ] || { echo "--file <mapping.csv> is required." >&2; exit 2; }
[ -f "$MAPPING_FILE" ] || { echo "mapping file not found: $MAPPING_FILE" >&2; exit 2; }
[ -n "$TENANT_ID" ] || { echo "--tenant <uuid> is required." >&2; exit 2; }
[ -n "$PARK_CODE" ] || { echo "--park <code> is required (sheds are resolved by name within one park)." >&2; exit 2; }

# A malformed row must fail the load, never import as a blank attribute.
if awk -F, 'NR>1 && (NF!=6 || $1=="" || $2=="" || $3=="" || $4=="" || $5=="" || $6=="")' "$MAPPING_FILE" | grep -q .; then
  echo "mapping file has malformed rows (expected: rfid,stage,shed_options,breed_options,age_band,sex)" >&2
  awk -F, 'NR>1 && (NF!=6 || $1=="" || $2=="" || $3=="" || $4=="" || $5=="" || $6=="")' "$MAPPING_FILE" >&2
  exit 1
fi

echo "database : ${DATABASE_URL%%\?*}"
echo "mapping  : $MAPPING_FILE ($(($(wc -l < "$MAPPING_FILE") - 1)) rows)"
echo "tenant   : $TENANT_ID   park: $PARK_CODE"
echo "mode     : $([ "$APPLY" -eq 1 ] && echo APPLY || echo 'DRY RUN (pass --apply to write)')"
echo "missing  : $([ "$CREATE_MISSING" -eq 1 ] && echo 'REGISTER as new animals' || echo 'skip (pass --create-missing to register)')"
echo

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q <<SQL
CREATE TEMP TABLE _map (rfid text PRIMARY KEY, stage text NOT NULL, shed_options text NOT NULL,
                        breed_options text NOT NULL, age_band text NOT NULL, sex text NOT NULL);
\copy _map FROM '$MAPPING_FILE' WITH (FORMAT csv, HEADER true)

\if $CREATE_MISSING
-- Register mapping rows that have no matching RFID yet. Explicitly opt-in: an attribute refresh
-- must never create animals by accident. dob / entry_date / origin_type are left NULL because the
-- roster does not carry them and inventing livestock history is worse than a null.
--
-- display_id is computed ONCE in _new_goats and used both to insert and to join the identifier
-- back to its goat. Matching on a deterministic key beats pairing two independent row_number()
-- sequences, which silently mis-pairs animals to RFIDs if either ordering ever changes.
BEGIN;
CREATE TEMP TABLE _new_goats AS
WITH next_id AS (
  SELECT COALESCE(max(NULLIF(regexp_replace(display_id, '\D', '', 'g'), '')::bigint), 0) AS n
  FROM goats WHERE tenant_id = '$TENANT_ID'::uuid
)
SELECT m.rfid, m.stage, m.age_band, m.sex,
       (string_to_array(m.breed_options,'|'))[1] AS breed,
       (string_to_array(m.shed_options,'|'))[1]  AS shed_name,
       'G-' || lpad((n.n + row_number() OVER (ORDER BY m.rfid))::text, 6, '0') AS display_id
FROM _map m CROSS JOIN next_id n
WHERE NOT EXISTS (SELECT 1 FROM goat_identifiers gi
                  WHERE gi.identifier_value = m.rfid AND gi.identifier_type = 'animal_identifier_1');

INSERT INTO goats (goat_id, tenant_id, display_id, species, breed, breed_id, sex,
                   lifecycle_status, age_band, management_stage, custodian_party_id, park_id, shed_id)
SELECT gen_random_uuid(), '$TENANT_ID'::uuid, ng.display_id,
       CASE WHEN ng.breed ILIKE '%sheep%' THEN 'sheep' ELSE 'goat' END,
       ng.breed,
       (SELECT b.breed_id FROM breeds b WHERE b.canonical_name = ng.breed
        ORDER BY (b.status='active') DESC LIMIT 1),
       ng.sex, 'alive', ng.age_band, ng.stage,
       (SELECT custodian_party_id FROM goats
        WHERE tenant_id = '$TENANT_ID'::uuid AND custodian_party_id IS NOT NULL LIMIT 1),
       p.location_id,
       (SELECT l.location_id FROM locations l
        WHERE l.tenant_id = '$TENANT_ID'::uuid AND l.location_type='shed'
          AND l.parent_location_id = p.location_id AND l.name = ng.shed_name LIMIT 1)
FROM _new_goats ng
JOIN locations p ON p.tenant_id = '$TENANT_ID'::uuid AND p.location_type='park' AND p.location_code='$PARK_CODE';

INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
                              scope_key, is_primary_for_goat, status, valid_from, source_system, normalizer_version)
SELECT '$TENANT_ID'::uuid, g.goat_id, 'animal_identifier_1', ng.rfid, ng.rfid,
       'tenant:' || '$TENANT_ID', true, 'active', now(), 'herd_mapping_load', 'v1'
FROM _new_goats ng
JOIN goats g ON g.tenant_id = '$TENANT_ID'::uuid AND g.display_id = ng.display_id;

-- Fail closed: every newly created animal must have got exactly one identifier.
DO \$\$
DECLARE orphans bigint;
BEGIN
  SELECT count(*) INTO orphans FROM goats g
  WHERE NOT EXISTS (SELECT 1 FROM goat_identifiers gi WHERE gi.goat_id = g.goat_id);
  IF orphans > 0 THEN
    RAISE EXCEPTION 'registration left % goat(s) with no identifier', orphans;
  END IF;
END \$\$;
COMMIT;
\endif

CREATE TEMP TABLE _resolved AS
WITH base AS (
  SELECT m.*, g.goat_id, g.shed_id AS cur_shed_id, cs.name AS cur_shed_name,
         g.breed AS cur_breed, g.species AS cur_species, g.age_band AS cur_age_band,
         g.management_stage AS cur_stage
  FROM _map m
  JOIN goat_identifiers gi ON gi.identifier_value = m.rfid
   AND gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
  JOIN goats g ON g.goat_id = gi.goat_id AND g.tenant_id = '$TENANT_ID'::uuid
   AND g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL
  LEFT JOIN locations cs ON cs.location_id = g.shed_id
)
SELECT b.goat_id, b.rfid, b.stage, b.age_band, b.sex, b.cur_stage, b.cur_shed_name, b.cur_breed,
       array_length(string_to_array(b.shed_options, '|'), 1)  AS shed_candidates,
       array_length(string_to_array(b.breed_options, '|'), 1) AS breed_candidates,
       (b.cur_shed_name = ANY(string_to_array(b.shed_options, '|')))  AS shed_kept,
       (b.cur_breed     = ANY(string_to_array(b.breed_options, '|'))) AS breed_kept,
       CASE WHEN b.cur_shed_name = ANY(string_to_array(b.shed_options, '|')) THEN b.cur_shed_id
            ELSE (SELECT l.location_id FROM locations l
                  JOIN locations p ON p.location_id = l.parent_location_id
                  WHERE l.tenant_id = '$TENANT_ID'::uuid AND l.location_type = 'shed'
                    AND p.location_code = '$PARK_CODE'
                    AND l.name = (string_to_array(b.shed_options, '|'))[1] LIMIT 1)
       END AS target_shed_id,
       CASE WHEN b.cur_breed = ANY(string_to_array(b.breed_options, '|')) THEN b.cur_breed
            ELSE (string_to_array(b.breed_options, '|'))[1]
       END AS target_breed
FROM base b;

\echo '== coverage =='
SELECT 'mapping rows' AS check, count(*)::text AS n FROM _map
UNION ALL SELECT 'resolved to a live goat', count(*)::text FROM _resolved
UNION ALL SELECT 'mapping rfid absent from db', (SELECT count(*) FROM _map m WHERE NOT EXISTS
   (SELECT 1 FROM _resolved r WHERE r.rfid = m.rfid))::text
UNION ALL SELECT 'shed could not be resolved', (SELECT count(*) FROM _resolved WHERE target_shed_id IS NULL)::text;

\echo ''
\echo '== ambiguity handling =='
SELECT 'multi-shed rows'  AS case, count(*) FILTER (WHERE shed_candidates > 1)::text AS n,
       count(*) FILTER (WHERE shed_candidates > 1 AND COALESCE(shed_kept, false))::text AS kept_current,
       count(*) FILTER (WHERE shed_candidates > 1 AND NOT COALESCE(shed_kept, false))::text AS forced_first
FROM _resolved
UNION ALL
SELECT 'multi-breed rows', count(*) FILTER (WHERE breed_candidates > 1)::text,
       count(*) FILTER (WHERE breed_candidates > 1 AND COALESCE(breed_kept, false))::text,
       count(*) FILTER (WHERE breed_candidates > 1 AND NOT COALESCE(breed_kept, false))::text
FROM _resolved;

\echo ''
\echo '== changes this load would make =='
SELECT 'stage'  AS field, count(*)::text AS changes FROM _resolved WHERE cur_stage IS DISTINCT FROM stage
UNION ALL SELECT 'shed', count(*)::text FROM _resolved r JOIN goats g USING (goat_id) WHERE g.shed_id IS DISTINCT FROM r.target_shed_id
UNION ALL SELECT 'breed', count(*)::text FROM _resolved WHERE cur_breed IS DISTINCT FROM target_breed
UNION ALL SELECT 'age_band', count(*)::text FROM _resolved r JOIN goats g USING (goat_id) WHERE g.age_band IS DISTINCT FROM r.age_band
UNION ALL SELECT 'sex', count(*)::text FROM _resolved r JOIN goats g USING (goat_id) WHERE g.sex IS DISTINCT FROM r.sex;

\echo ''
\echo '== resulting distribution =='
SELECT stage, target_breed AS breed,
       CASE WHEN target_breed ILIKE '%sheep%' THEN 'sheep' ELSE 'goat' END AS species,
       age_band, count(*) AS goats
FROM _resolved GROUP BY 1,2,3,4 ORDER BY 5 DESC;

\if $APPLY
\echo ''
\echo '== applying =='
BEGIN;

-- Fail closed on AMBIGUITY (review follow-up #4). Where a mapping row lists more than one shed or
-- breed candidate and the animal's current value is NOT among them, the resolver above falls back
-- to the FIRST candidate -- an invented truth. Guessing owner/location/breed on --apply corrupts
-- the live herd, so abort the whole transaction instead. The '== ambiguity handling ==' report
-- above (dry-run) lists the forced_first counts; disambiguate the source and re-run.
DO \$\$
DECLARE ambiguous bigint;
BEGIN
  SELECT count(*) INTO ambiguous FROM _resolved
  WHERE (shed_candidates  > 1 AND NOT COALESCE(shed_kept, false))
     OR (breed_candidates > 1 AND NOT COALESCE(breed_kept, false));
  IF ambiguous > 0 THEN
    RAISE EXCEPTION 'refusing --apply: % row(s) have ambiguous shed/breed and would be forced to the first candidate (guessed truth). Disambiguate the source and re-run.', ambiguous;
  END IF;
END \$\$;

-- TODO(counts-followup #4): this direct UPDATE of goats bypasses the canonical identity producer,
-- so it emits no goat.location.changed / lifecycle identity event, writes no outbox_messages row,
-- and does not re-scope shed-scoped vaccination obligations. Route these writes through the
-- registered identity import/relocate producer (see domain-event-registry.json) before this tool
-- is used for anything beyond a disambiguated dev backfill.
INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order, status)
SELECT DISTINCT '$TENANT_ID'::uuid, r.stage, r.stage, 0, 'active' FROM _resolved r
ON CONFLICT (tenant_id, stage_code) DO NOTHING;

UPDATE goats g
SET management_stage = r.stage,
    shed_id  = COALESCE(r.target_shed_id, g.shed_id),
    breed    = r.target_breed,
    breed_id = COALESCE((SELECT b.breed_id FROM breeds b WHERE b.canonical_name = r.target_breed
                         ORDER BY (b.status = 'active') DESC LIMIT 1), g.breed_id),
    species  = CASE WHEN r.target_breed ILIKE '%sheep%' THEN 'sheep' ELSE 'goat' END,
    age_band = r.age_band,
    sex = r.sex,
    updated_at = now(),
    row_version = g.row_version + 1
FROM _resolved r
WHERE g.goat_id = r.goat_id;

COMMIT;

\echo ''
\echo '== live herd after load =='
SELECT COALESCE(NULLIF(g.management_stage,''),'(none)') AS stage,
       COALESCE(NULLIF(g.breed,''),'(none)') AS breed, g.species, g.age_band, count(*) AS goats
FROM goats g
WHERE g.tenant_id = '$TENANT_ID'::uuid AND g.lifecycle_status = 'alive' AND g.merged_into_goat_id IS NULL
GROUP BY 1,2,3,4 ORDER BY 5 DESC;
\endif
SQL

if [ "$APPLY" -eq 1 ]; then
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -c 'ANALYZE goats;'
  echo; echo "applied. planner stats refreshed."
else
  echo; echo "dry run only — nothing was written. Re-run with --apply to commit."
fi

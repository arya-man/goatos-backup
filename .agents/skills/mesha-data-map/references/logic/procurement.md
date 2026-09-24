# Procurement logic cards

Index: P1 loads by status | P2 animals per load / intake (pipeline) | P3 intake variance & evidence (source entry) | P4 load landed cost (animal/transport/other) | P5 landed cost per kg | P6 loads entered in a period | P7 animal-purchase candidates (pending/accepted/rejected) | P8 vendor register count by status / type | P9 load animals by park.
Load-wise sales (sold, remaining, P&L per load): not here, use `references/load-wise-sales.sql`.

Source tables: `public.procurement_loads` (1 row per load; status, expected_count, purchase_date, animal_cost, transport_cost, other_cost, purchase_weight_kg, context->>'load_ref', context->>'farm'), `public.procurement_load_goats` (1 row per animal on a load; current_state, selection_state), `public.animal_purchase_loads` / `animal_purchase_candidates` (pre-purchase inspection), `public.procurement_vendors` (contact register). ceo_ai views: `procurement_pipeline`, `procurement_loads_base`, `source_entry_health_status`.

## P1 Loads by status
- Screen: Procurement > Source entry board (/procurement/source-entry), Sales > Loads list.
- Endpoint: GET /procurement/source-entry/loads?status=&limit=200
- Code: backend/internal/procurement/adapters/postgres/repository.go:65 (ListLoads)
- Formula: rows of procurement_loads JOIN parties (source), optional `pl.status = $status`, ordered updated_at DESC. No count is computed server-side; the board counts rows.
- Filters->SQL: status chip -> `status = '<x>'`. No park or date filter on this endpoint.
- Traps: list is paged (limit 200) - count in SQL, not by page. Inner JOIN parties: a load without source party would vanish from the screen (none on stg).
- SQL: `SELECT status, count(*) FROM public.procurement_loads GROUP BY 1;`
- stg 24/09/2026: accepted_intake 9 (all 9 loads are fully intaken; no draft/in-transit loads).
- CEO asks: "How many loads are in progress?" / "How many loads have we bought so far?" / "Kitne loads abhi transit mein hain?" / "Total kitne load aaye ab tak?"

## P2 Animals per load / pipeline stage
- Screen: Load detail (/procurement/source-entry/[load]) goat rows; Ask Mesha pipeline.
- Endpoint: GET /procurement/source-entry/loads/{load_id}
- Code: repository.go:207 (GetLoadDetail); view ceo_ai.procurement_pipeline (migrations/postgres/000001_goatos_clean_slate_baseline.sql:15397)
- Formula: animals = count(procurement_load_goats) per load; rejected = count where current_state='rejected' OR selection_state='rejected'; vaccination_pending = HF evidence with review_status in (pending, unreviewed, needs_review). current_stage = procurement_loads.status.
- Filters->SQL: none (current state); period -> use entered_at.
- Traps: `expected_count` (659 total) is what was planned/bought, NOT animals attributed on the farm (407). 252 animals on old loads were never tagged into procurement_load_goats (legacy loads before per-goat entry). Say which number you give. batch_label is a synthesized 'Load <uuid8> · DD Mon', not the business load number; use context->>'load_ref' (100, 101, 113, 126...).
- SQL: `SELECT current_stage, count(*) loads, sum(animals) animals, sum(rejected) rejected, sum(vaccination_pending) vacc_pending FROM ceo_ai.procurement_pipeline GROUP BY 1;`
- stg 24/09/2026: accepted_intake | 9 loads | 407 animals | 0 rejected | 0 vacc pending. (`animals` = count of ALL procurement_load_goats rows on the load, no state filter; it equals the accepted-herd figure only because all 407 are current_state='accepted_herd_intake' today.)
- CEO asks: "How many animals came in on load 136?" / "Any rejections in procurement?" / "Load 136 mein kitne janwar aaye?" / "Kitne animals reject hue?"

## P3 Intake variance and evidence (source entry health)
- Screen: Source entry board / load detail arrival gate (expected vs arrived vs accepted), evidence badge.
- Endpoint: GET /procurement/source-entry/loads/{load_id} (arrival_reviews, source_health)
- Code: ceo_ai.source_entry_health_status (000001 baseline:15435, redefined 000359_ceo_ai_readonly_role_ceo_ai_only.sql:40)
- Formula: animals_expected = coalesce(arrival_intake_reviews.expected_count, loads.expected_count); received = arrived_count; accepted = matched_count; rejected = rejected_count; health_blockers = source health checks with health_state in (blocked, failed, sick, quarantine); evidence_status = no_evidence / evidence_pending / evidence_complete.
- Filters->SQL: per load (load_label, source_label). Variance = animals_expected - animals_received.
- Traps: on stg arrival_intake_reviews is empty -> received/accepted/rejected are NULL for all 9 loads. NULL means "arrival gate not recorded", NOT zero arrived. Do not report 100% loss. evidence_status = no_evidence on all loads (HF vaccination evidence not captured) - not a failure.
- SQL: `SELECT load_label, source_label, animals_expected, animals_received, animals_accepted, animals_rejected, animals_expected - animals_received AS variance, health_blockers, evidence_status FROM ceo_ai.source_entry_health_status;`
- stg 24/09/2026: 9 loads, expected 659, received/accepted/rejected NULL (not recorded), health_blockers 0, all no_evidence.
- CEO asks: "Did every load arrive with the full count?" / "Any health issues at source?" / "Load mein jitne bole utne aaye kya?" / "Source pe koi bimar janwar mila?"

## P4 Load landed cost (animal + transport + other)
- Screen: Sales > Loads (sales-loads.tsx) cost columns; Load cost drawer (load-cost-drawer.tsx).
- Endpoint: GET /procurement/loadwise-sales (read), PUT /procurement/loads/{load_id}/cost (write).
- Code: backend/internal/procurement/adapters/postgres/loadwise_repository.go:270 (cost columns); domain/loadwise.go:361 (loadPurchaseValue)
- Formula: purchase value (landed) = animal_cost + coalesce(transport_cost,0) + coalesce(other_cost,0); NULL when animal_cost is NULL (cost not recorded). Schema forbids transport/other without animal_cost.
- Filters->SQL: park chip -> load farm = the park all its accepted animals agree on (else context->>'farm' for loads with no attributed animals); mixed/unknown-park loads only show under All Parks.
- Traps: NULL animal_cost = "not costed", never 0. Rupees, not lakhs. Cost lines (itemisation) are a separate 1:N read - never join them into the load row (fan-out).
- SQL: `SELECT context->>'load_ref' load_ref, purchase_date, animal_cost, transport_cost, other_cost, animal_cost + coalesce(transport_cost,0) + coalesce(other_cost,0) AS landed_cost FROM public.procurement_loads ORDER BY purchase_date;`
- stg 24/09/2026: 9/9 costed; totals animal 56,88,813 + transport 2,58,784 + other 63,160 = landed 60,10,757 INR. Latest load 136 (16/09/2026): 5,24,032.
- CEO asks: "What did load 136 cost us all-in?" / "How much have we spent on transport for loads?" / "Load 136 ka total kharcha kitna?" / "Transport pe kitna gaya ab tak?"

## P5 Landed cost per kg
- Screen: Sales > Loads "Landed Rs/kg" column; cost drawer preview.
- Endpoint: GET /procurement/loadwise-sales
- Code: domain/loadwise.go:385 (landedPricePerKg), used at :326
- Formula: landed cost (P4) / purchase_weight_kg; NULL if either is NULL or weight <= 0.
- Filters->SQL: same park rule as P4.
- Traps: per-kg on animal_cost alone understates cost - always landed. For an all-loads figure use sum(cost)/sum(kg), never the average of per-load rates.
- SQL: `SELECT context->>'load_ref', round((animal_cost+coalesce(transport_cost,0)+coalesce(other_cost,0))/nullif(purchase_weight_kg,0),2) AS landed_per_kg FROM public.procurement_loads WHERE animal_cost IS NOT NULL;` -- overall: `SELECT round(sum(animal_cost+coalesce(transport_cost,0)+coalesce(other_cost,0))/sum(purchase_weight_kg),2) FROM public.procurement_loads WHERE animal_cost IS NOT NULL AND purchase_weight_kg > 0;`
- stg 24/09/2026: per load 413.67 (load 100) to 463.54 (load 128); load 136 = 460.00; weighted overall 440.16 Rs/kg over 13,655.7 kg.
- CEO asks: "What's our landed cost per kg?" / "Which load was cheapest per kg?" / "Per kg landed rate kya pada?" / "Sabse sasta load kaunsa tha?"

## P6 Loads entered in a period
- Screen: none directly (Ask Mesha period question).
- Endpoint: n/a
- Code: ceo_ai.procurement_loads_base (000001 baseline:16354)
- Formula: one row per load; entered_business_day = IST date of created_at; purchase_date = business purchase date.
- Filters->SQL: "this month" -> `entered_business_day >= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date`.
- Traps: legacy loads were back-entered: created_at (31/08-18/09/2026) is NOT when they were bought (purchase_date goes back to 21/10/2025). For "loads bought in X" use purchase_date. source_label is NULL (no source_location) - vendor name is in parties via procurement_loads.source_party_id.
- SQL: `SELECT count(*) FROM public.procurement_loads WHERE purchase_date >= date_trunc('month', now() AT TIME ZONE 'Asia/Kolkata')::date;`
- stg 24/09/2026: 1 load purchased in Sep 2026 (load 136, 16/09/2026, 58 expected); 1 entered in Sep by entered_business_day too.
- CEO asks: "How many loads did we buy this month?" / "When was the last load bought?" / "Is mahine kitne load kharide?" / "Last load kab aaya?"

## P7 Animal purchase candidates (inspection decisions)
- Screen: Procurement > Animal purchases (/procurement/animal-purchases) review queue chips Pending / Accepted / Rejected / All; load list with per-load counts.
- Endpoint: GET /procurement/animal-purchases/review?load_id=&decision=&recorded_from=&recorded_to=; GET /app/procurement/animal-purchases/loads
- Code: backend/internal/animalpurchase/adapters/postgres/repository.go:176 (review counts), :86 (per-load counts), :627 (ListReview)
- Formula: count(*) FILTER (decision='pending' / 'accepted' / 'rejected') over animal_purchase_candidates. Chip counts are computed BEFORE the decision filter and cursor (whole-filter).
- Filters->SQL: load -> `c.load_id = ...`; recorded dates (IST) -> `c.created_at >= from AND c.created_at < to+1`; decision chip only filters rows, not counts. Default chip = pending.
- Traps: these are inspection candidates before purchase; separate from procurement_loads (no FK). Accepted candidate != animal on farm.
- SQL: `SELECT count(DISTINCT l.load_id) loads, count(c.*) candidates, count(*) FILTER (WHERE c.decision='pending') pending, count(*) FILTER (WHERE c.decision='accepted') accepted, count(*) FILTER (WHERE c.decision='rejected') rejected FROM public.animal_purchase_loads l LEFT JOIN public.animal_purchase_candidates c ON c.load_id = l.load_id;`
- stg 24/09/2026: 2 open purchase loads, 62 candidates: 0 pending, 62 accepted, 0 rejected.
- CEO asks: "How many animals are waiting for purchase approval?" / "What's our rejection rate at inspection?" / "Kitne janwar approval ke liye pending hain?" / "Inspection mein kitne reject kiye?"

## P8 Vendor register
- Screen: Procurement > Vendors (/procurement/vendors) and Sales > Vendors register (same board): "<N> vendors" tag, status tag per row.
- Endpoint: GET /procurement/vendors?search=&record_type=&status=&state=&city=&breed=&limit=&offset=
- Code: backend/internal/procurement/adapters/postgres/vendor_repository.go:110 (buildVendorFilter), :187 (ListVendors), :235 (total)
- Formula: total = count(*) over procurement_vendors with the same WHERE as the page (whole-filter). Vendor options for new trades: status='active' only (:540).
- Filters->SQL: record_type -> `record_type = x`; status -> `status = x` (active / inactive / negotiating / banned); search -> `search_text LIKE '%x%'`; state/city/breed filters on those columns.
- Traps: register mixes every counterparty type (butchers = buyers, feed suppliers, transport). "Animal vendors" = Sheep Agent, Goats Agent, Agent, Goat/Sheep Stockist, Goat Farm, Farmer, Breeding Agent - state which types you counted. Bank/PAN fields are finance-gated; do not surface them.
- SQL: `SELECT status, count(*) FROM public.procurement_vendors GROUP BY 1;` / `SELECT record_type, count(*) FROM public.procurement_vendors GROUP BY 1 ORDER BY 2 DESC;`
- stg 25/09/2026: 688 total: active 623, inactive 61, negotiating 4 (1 Company added 25/09 00:27 IST). Top types: Butcher 192, Farmer 109, Sheep Agent 89, Agent 61, Transport Agent 56.
- CEO asks: "How many active vendors do we have?" / "How many sheep agents are registered?" / "Kitne active vendor hain?" / "Butcher kitne register hain?"

## P9 Load animals by park
- Screen: Sales > Loads park chip (CBE / CPT) and Farm column.
- Endpoint: GET /procurement/loadwise-sales?park_id=
- Code: loadwise_repository.go:67 (accepted_herd_intake members), farm CASE at ~:290
- Formula: animals on loads with current_state='accepted_herd_intake', park via goats.park_id. A load belongs to a park only if every attributed animal is in that one park.
- Traps: current park, not park at arrival (transfers move them). Sold/dead animals keep their last park_id.
- SQL: `SELECT l.name park, count(DISTINCT plg.goat_id) FROM public.procurement_load_goats plg JOIN public.goats g ON g.goat_id = plg.goat_id JOIN public.locations l ON l.location_id = g.park_id WHERE plg.current_state = 'accepted_herd_intake' GROUP BY 1;`
- stg 24/09/2026: Coimbatore 263, Channapatna 144 (total 407).
- CEO asks: "How many procured animals are in Channapatna?" / "Which park got load 136?" / "Channapatna mein kitne procured janwar hain?" / "Load 136 kis farm gaya?"

## P10 Source entry board row (supplier warmup table + load drawer)
- Screen: Procurement > Source entry (/procurement/source-entry) table "Supplier warmup — Holding Farm" (cols: Load, Holding farm / supplier, Purpose, Animals, Warmup, Tagging, Vaccination (HF), Health / selection, Status) and the row drawer (same facets + "Goats in load").
- Endpoint: GET /procurement/source-entry/loads?status=&limit=200 (row identity, expected_count, purchase_date, status); GET /procurement/source-entry/loads/{load_id} (via /api/.../drawer) for goats[] and hf_vaccination_evidence[] facets.
- Code: apps/admin-web/features/procurement/source-entry-board.tsx:36 (warmupCell), :62 (healthSelectionLabel), :112 (purposeLabel), :120 (taggingLabel), :127 (hfVaccinationLabel); source-entry-local-drawer.tsx:131 (drawerItemFromDetail); work-state.ts:186 (warmupExpectation 28-35d), :198 (warmupMeta); backend/internal/procurement/adapters/postgres/repository.go:65 (ListLoads), :207 (GetLoadDetail), :2375 (listLoadGoats), :2424 (listHFVaccinationEvidence); handler.go:54/:56.
- Formula (all computed in the browser from the two reads):
  - Holding farm = source location name/code, else "Holding farm not set"; supplier = parties.display_name.
  - Purpose = the single distinct procurement_load_goats.purpose, "Mixed" if >1, "-" if none.
  - Animals = procurement_loads.expected_count; Goats in load (drawer) = count(procurement_load_goats).
  - Tagging = count(goats with BOTH animal_identifier_1 AND animal_identifier_2) / expected_count.
  - Warmup days = max(goat.warmup_days) else whole days since purchase_date (UTC midnight); band vs 28-35d: >35 warn "outside window", <28 info "before window", else ok. Mixed purpose -> "<d>d · Mixed".
  - Vaccination (HF) = flagged if any evidence rejected/conflicting, else trusted if any trusted, else imported if any imported, else due.
  - Health / selection = status map: source_warmup->warming, health_pending->health_pending, pre_dispatch_pending/dispatch_ready->selection_ok, rejected/blocked->blocked_rejected, deferred->review, everything else (incl. accepted_intake)->cleared_forward.
- Filters->SQL: status chip -> `pl.status = x` (only server filter). Search box / filter drawer act on visible rows only. No park/date filter.
- Traps: on the board itself the facet cells show "-" until a row's drawer is opened (the list API carries no goats). Tagging "0/58" does NOT mean untagged: all 407 animals have identifier_1, only 4 have identifier_2. Warmup falls back to days-since-purchase because warmup_days is NULL on every goat - so every legacy load reads "outside window". HF "due" on every load = no evidence captured, not overdue vaccination. Holding farm is not set on any load (source_location_id NULL).
- SQL:
  SELECT pl.context->>'load_ref' AS load_ref, p.display_name AS supplier,
    coalesce(loc.name, loc.location_code, 'Holding farm not set') AS holding_farm,
    CASE WHEN count(DISTINCT g.purpose) FILTER (WHERE g.purpose <> '') = 0 THEN '-' WHEN count(DISTINCT g.purpose) FILTER (WHERE g.purpose <> '') > 1 THEN 'mixed' ELSE max(g.purpose) END AS purpose,
    pl.expected_count, count(g.load_goat_id) AS goats_in_load,
    count(*) FILTER (WHERE coalesce(g.animal_identifier_1,'') <> '' AND coalesce(g.animal_identifier_2,'') <> '') || '/' || pl.expected_count AS tagging,
    coalesce(max(g.warmup_days), (now() AT TIME ZONE 'UTC')::date - pl.purchase_date) AS warmup_days,
    CASE WHEN coalesce(max(g.warmup_days), (now() AT TIME ZONE 'UTC')::date - pl.purchase_date) > 35 THEN 'outside 28-35d' WHEN coalesce(max(g.warmup_days), (now() AT TIME ZONE 'UTC')::date - pl.purchase_date) < 28 THEN 'before window' ELSE 'in window' END AS warmup_band,
    (SELECT CASE WHEN bool_or(e.review_status IN ('rejected','conflicting')) THEN 'flagged' WHEN bool_or(e.review_status='trusted') THEN 'trusted' WHEN bool_or(e.review_status='imported') THEN 'imported' ELSE 'due' END FROM public.procurement_hf_vaccination_evidence e WHERE e.load_id = pl.load_id) AS hf_vaccination,
    CASE pl.status WHEN 'source_warmup' THEN 'warming' WHEN 'health_pending' THEN 'health_pending' WHEN 'pre_dispatch_pending' THEN 'selection_ok' WHEN 'dispatch_ready' THEN 'selection_ok' WHEN 'rejected' THEN 'blocked_rejected' WHEN 'blocked' THEN 'blocked_rejected' WHEN 'deferred' THEN 'review' ELSE 'cleared_forward' END AS health_selection,
    pl.status
  FROM public.procurement_loads pl JOIN public.parties p ON p.party_id = pl.source_party_id
  LEFT JOIN public.locations loc ON loc.location_id = pl.source_location_id
  LEFT JOIN public.procurement_load_goats g ON g.load_id = pl.load_id
  GROUP BY pl.load_id, p.display_name, loc.name, loc.location_code ORDER BY pl.updated_at DESC;
- stg 24/09/2026: 9 rows, all accepted_intake / cleared_forward / HF due / holding farm not set. Load 136 Sai Vinay: fattening, 58/58 goats, tagging 0/58, 8d "before window". Loads 131,126,128,129,101,130,100: purpose unspecified, 94-338d outside window. Load 113 (Nutriplus): 0 goats, purpose "-", tagging 0/100. Tagged total 4 (101: 3/70, 100: 1/76).
- CEO asks: "Which loads are still in their warmup window?" / "How many animals on load 136 are fully tagged?" / "Kaunse load abhi warmup mein hain?" / "Load 136 mein kitne janwar ka tag poora hua?"

## P11 Load detail - animal states and journey tables
- Screen: /procurement/source-entry/loads/[load_id]: header chips (status, Expected, Purchase, Planned dispatch); "Animals in load" (Animal IDs, Selection, Current stage, Source entry, Ownership, Health, Warmup, Downstream); Pre-dispatch decisions; Arrival gate (matched/missing/extra/reviewed per review + goat rows); Transit handoffs; Holding stays; Source health checks; Accepted intake -> PC handoffs; HF evidence table (load-forms.tsx); journey timeline.
- Endpoint: GET /procurement/source-entry/loads/{load_id} -> detail.load, goats[], decisions[], arrival_reviews[].goats[], transit_handoffs[], holding_stays[], source_health_checks[], pc_handoffs[], hf_vaccination_evidence[], timeline[].
- Code: load-detail.tsx:179 (GoatRows), :263 (DecisionCard), :311/:329-331 (ArrivalGateCard counts), :377 (Transit), :414 (Holding), :459 (Health), :495 (PC handoffs), :140 (Timeline), :618-621 (header); load-forms.tsx:316 (HF evidence table). Backend repository.go:207 (GetLoadDetail), :2375 listLoadGoats, :2401 listHoldingStays, :2424 listHFVaccinationEvidence, :2449 listHealthChecks, :2472 listDecisions, :2496 listTransit, :2607 listArrivalReviews, :2642 listArrivalGoats, :2665 listPCHandoffs, :2692 buildTimeline.
- Formula: 1 row per procurement_load_goats row, shown with its stored states (selection_state, current_state, source_entry_state, ownership_state, health_state; warmup as in P10 per goat). Downstream = goat page link if current_state is accepted intake, "history, no PC" if history-only state, else "in source entry". Arrival card per review: matched = arrival_state in (matched, accepted); missing = 'missing'; extra = 'extra_unresolved'; reviewed = all review goats. The other tables are straight rows from their tables filtered by load_id.
- Filters->SQL: `load_id = <uuid>` or `procurement_loads.context->>'load_ref' = '<ref>'`.
- Traps: every journey table is EMPTY on stg (decisions, transit, arrival reviews, holding, health checks, PC handoffs, HF evidence = 0 rows): legacy loads were intaken straight to accepted_herd_intake. Empty does not mean "nothing went wrong". source_entry_state/ownership/health 'pending' on 144 animals (loads 100,101,129,131) is a data gap, not a pending action. Timeline is Go-only: buildTimeline merges load_created + one event per goat/stay/check/decision/transit/review/handoff, sorted by time. Reproduce with a UNION ALL of those tables' timestamps if needed.
- SQL: `SELECT pl.context->>'load_ref' load_ref, g.current_state, g.selection_state, g.source_entry_state, g.ownership_state, g.health_state, count(*) animals FROM public.procurement_load_goats g JOIN public.procurement_loads pl ON pl.load_id=g.load_id WHERE pl.context->>'load_ref'='129' GROUP BY 1,2,3,4,5,6;` -- journey tables: `SELECT (SELECT count(*) FROM public.source_entry_decisions) decisions, (SELECT count(*) FROM public.transit_handoffs) transit, (SELECT count(*) FROM public.arrival_intake_reviews) arrival, (SELECT count(*) FROM public.source_holding_stays) holding, (SELECT count(*) FROM public.procurement_source_health_checks) health, (SELECT count(*) FROM public.procurement_pc_handoffs) pc_handoffs, (SELECT count(*) FROM public.procurement_hf_vaccination_evidence) hf;`
- stg 24/09/2026: 407 animals all accepted_herd_intake. 263 accepted/mesha_owned/passed (loads 126,128,130,136); 144 pending/pending/pending (129: 77, 131: 63, 101: 3, 100: 1). All 7 journey tables 0 rows. No planned_dispatch_at on any load.
- CEO asks: "Did any animals on load 129 fail the source health check?" / "Was anything pulled off a load before dispatch?" / "Load 129 ke janwar ka health check hua kya?" / "Dispatch se pehle koi janwar nikala kya?"

## P12 Animal purchase loads list and animal facts
- Screen: /procurement/animal-purchases: KPI tiles (Loads, Awaiting decision, Accepted, Rejected); Loads table (Load, Vendor, Farm, Expected, Recorded, Awaiting, Accepted, Rejected, Added on); selected-load panel (status Open/Closed, Recorded by, Added on, Note); filter bar (Load, Recorded from/to); decision chips with counts; animal cards (title, decision tag, field verdict, Breed, Age, Weight, Looks, Temporary tag, Note, Decided by/on, SOP answers + media).
- Endpoint: GET /app/procurement/animal-purchases/loads?limit=20 (loads[].counts, load_ref, vendor_name, farm, expected_count, status, recorded_by_name, created_at, notes); GET /procurement/animal-purchases/review?decision=all&limit=1 (tiles = counts); GET /procurement/animal-purchases/review?load_id=&decision=&recorded_from=&recorded_to= (animals[], filters[].count).
- Code: animal-purchases.tsx:90-104 (reads), :189-210 (tiles), :314-324 (load row), :245-278 (load panel), :362-413 (filters/chips), :449-517 (animal card); backend/internal/animalpurchase/adapters/postgres/repository.go:72 (loadColumns), :83 (loadCountsJoin), :175 (sqlReviewCountsBase), :297 (ListLoads), :627 (ListReview); app/service.go:234/:257 (IST window); adapters/http/handler.go:39/:44; payloads.go:206 (load), :266 (candidate title/labels).
- Formula: Loads tile = rows on the first loads page (20), "+" if more, NOT a DB count. Other tiles = whole-desk count(*) FILTER by decision over animal_purchase_candidates (= P7). Per-load Recorded/Awaiting/Accepted/Rejected = the same FILTERs per load_id. Farm = animal_purchase_loads.farm_label. Recorded by = active workforce_members.display_name of recorded_by.
- Filters->SQL: load -> `c.load_id`; recorded from/to (IST dates, inclusive) -> `c.created_at >= from::timestamp AT TIME ZONE 'Asia/Kolkata' AND c.created_at < (to+1)::timestamp AT TIME ZONE 'Asia/Kolkata'`; decision chip filters rows only, chip counts are whole-filter.
- Traps: load_ref is free text and COLLIDES with procurement_loads: purchase load "136" (Kishor Satya Salli, 15 sheep) is NOT source-entry load 136 (Sai Vinay, 58 goats). Always say which. Loads tile is capped by page size. All 62 candidates are male sheep with no breed, age or condition, so averages by breed or age are meaningless. questionnaire_version = 0 on both loads (legacy card, not SOP). field_verdict (buying desk) is not the CEO decision.
- SQL: `SELECT l.load_ref, l.vendor_name, l.farm_label, l.expected_count, l.status, (l.created_at AT TIME ZONE 'Asia/Kolkata')::date added_on, count(c.*) recorded, count(*) FILTER (WHERE c.decision='pending') awaiting, count(*) FILTER (WHERE c.decision='accepted') accepted, count(*) FILTER (WHERE c.decision='rejected') rejected, round(sum(c.weight_kg),1) kg FROM public.animal_purchase_loads l LEFT JOIN public.animal_purchase_candidates c ON c.load_id=l.load_id GROUP BY l.load_id ORDER BY l.created_at DESC;` -- date window: `SELECT count(*), count(*) FILTER (WHERE c.decision='accepted') FROM public.animal_purchase_candidates c WHERE c.created_at >= ('2026-09-15'::date::timestamp AT TIME ZONE 'Asia/Kolkata') AND c.created_at < (('2026-09-15'::date+1)::timestamp AT TIME ZONE 'Asia/Kolkata');`
- stg 24/09/2026: 2 loads, both CBE, open, added 14/09/2026. 134 Sai Vinay: expected 60, recorded 47, accepted 47, 837.4 kg (15.0-22.3 kg). 136 Kishor Satya Salli: expected 20, recorded 15, accepted 15, 307.6 kg. Tiles: 0 awaiting, 62 accepted, 0 rejected. Recorded on 15/09 IST: 10 (all accepted). All decided by Manohark, 14-18/09. Field verdict "selected" on all 62, no decision notes.
- CEO asks: "How many animals did we accept on purchase load 134 and what do they weigh?" / "How many animals did the buying desk record last week?" / "Load 134 mein kitne accept kiye, total weight kitna?" / "Pichle hafte kitne janwar film kiye?"

## P13 Vendor register by side, facets and profile fields
- Screen: /procurement/vendors and /sales/vendors (same board, different side): "<N> vendors" tag, table (Business name + contact, Record type, Phone, Location, Status), filter bar (search, Record type, Status, State, City, Breed), vendor drawer (record type, status, contact, phone, state, city, breed, feed, filtered stock, price/goat, avg animal weight, ETA after order, ready to filtered, details, capacity, comments, extra form answers, voice note, bank/PAN finance-gated).
- Endpoint: GET /procurement/vendors?side=procurement|sales&search=&record_type=&status=&state=&city=&breed=&limit=&offset= (vendors[], total); GET /procurement/vendor-catalog?side= (filter options); GET /procurement/vendors/{id}.
- Code: vendor-board.tsx:68 (sideFromContract), :107 (reads), :184 (total tag), :212-238 (row); vendor-filter-bar.tsx:13-19 (facets); vendor-local-drawer.tsx:413-468; backend adapters/postgres/vendor_repository.go:116 (buildVendorFilter), :159 (side predicate), :187 (ListVendors), :479 (ListVendorCatalog, city derived live); adapters/http/vendor_payloads.go:294 (status_label), :302/:353 (location_display = "city, state"); domain/vendor.go:472 (average_animal_weight_display), :491 (capacity_display); adminui/app/service.go:595/:660 (side in table data_source).
- Formula: side = sales when procurement_vendor_catalog has kind='record_type', value=record_type and register_side='sales'; everything else (including record types not in the catalog) = procurement. The two sides together cover every vendor, and no vendor is on both. Total = count(*) with the same WHERE.
- Filters->SQL: as P8, plus side predicate. City options = DISTINCT btrim(city) from the register; other options from the catalog.
- Traps: "Agent" is a SALES record type (catalog), not a supplier - contradicts P8's animal-vendor list. Sales side is Butcher/Agent/Company/Slaughter House only (Slaughter House has 0 rows). "Farmer" is procurement side despite the sales subtitle. Profile fields are sparse: price_per_goat 0 filled, avg weight / ETA / filtered stock 13, capacity 20, voice notes 0. Never quote bank/PAN.
- SQL: `WITH v AS (SELECT v.*, CASE WHEN EXISTS (SELECT 1 FROM public.procurement_vendor_catalog c WHERE c.tenant_id=v.tenant_id AND c.kind='record_type' AND c.value=v.record_type AND c.register_side='sales') THEN 'sales' ELSE 'procurement' END side FROM public.procurement_vendors v) SELECT side, status, count(*) FROM v GROUP BY 1,2 ORDER BY 1,2;` -- by type: same CTE `SELECT side, record_type, count(*) FROM v GROUP BY 1,2 ORDER BY 1,3 DESC;` -- facets: `SELECT state, count(*) FROM public.procurement_vendors GROUP BY 1 ORDER BY 2 DESC;`
- stg 24/09/2026: procurement 422 (active 360, inactive 58, negotiating 4); sales 266 (active 263, inactive 3) on 25/09 (was 265/262 on 24/09). Procurement top: Farmer 109, Sheep Agent 89, Transport Agent 56, Manure Agent 37, Feed Agent 31. Sales: Butcher 192, Agent 61, Company 13 (25/09). States: TN 274, KA 115, AP 110, MP 63, KL 43. 117 distinct cities. Top breeds: Any Breed 14, Anantapur Sheep 9, Dorper 9.
- CEO asks: "How many active suppliers are on the procurement side versus buyers on the sales side?" / "How many sheep agents do we have in Karnataka?" / "Kharidne wale aur bechne wale vendor kitne hain alag alag?" / "Karnataka mein kitne sheep agent hain?"

## P8 correction (vendor types)
- "Agent" is a SALES-side record type (procurement_vendor_catalog register_side='sales', stg 61 rows; see P13), not an animal supplier: drop it from P8's "animal vendors" list.

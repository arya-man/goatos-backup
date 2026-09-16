# Backend and database business-configuration audit

Current source `e06d27bf600b4d5d7c46d178d9341a5f6c942a6a`; read-only source and real staging. This is a practical inventory of concrete business values, defaults, clinical rules, schedules and constraints—not a claim every line/literal was semantically audited. Technical timeouts, cache TTLs, pagination sizes, UI lengths, mathematical unit conversions and security nonces are deliberately excluded from the curated list.

**Requested ownership:** all valuation rates and assumed weights and the30/35kg reporting settings should be authored under Sales Config and shared with their consumers, per the latest user clarification. Current source still places them across Sales SQL, Weighing domain and backend UI copy. Keep valuation, readiness reporting, eligibility enforcement, market benchmarks and actual sales separate even with one Sales owner. This is research, not an implementation or migration.

Classification: hardcoded = literal/committed policy in source (may be fallback); DB-seed/editable = observed persisted configuration with existing specialized authoring; DB-stored/UI-unverified = persisted but current editing control not established; domain invariant = validation/identity guard, not automatically a CEO-adjustable setting; unverified effective precedence = conflicting storage surfaces need resolver proof. “Hardcoded” does not mean safe to generalize or delete.

## Curated actionable inventory

| ID | Owner | Setting / observed value | Classification | Exact evidence | Interpretation |
|---|---|---|---|---|---|
| B001 | Sales Config | Fattening valuation — 450 INR/kg | hardcoded | `backend/internal/sales/adapters/postgres/overview_repository.go:619` | Proposed owner Sales Config; shared valuation consumers, distinct from actual sale price. |
| B002 | Sales Config | Adult female valuation — 40 kg ×600 INR/kg | hardcoded | `backend/internal/sales/adapters/postgres/overview_repository.go:620` | Both assumed weight and rate need separate Sales-owned values. |
| B003 | Sales Config | Adult male valuation — 60 kg ×500 INR/kg | hardcoded | `backend/internal/sales/adapters/postgres/overview_repository.go:621` | Not observed individual weight. |
| B004 | Sales Config | Kid valuation — K0/K1=3kg;K2=8kg;K3=15kg;500 INR/kg | hardcoded | `backend/internal/sales/adapters/postgres/overview_repository.go:622` | Age/stage valuation matrix, not physical measurement. |
| B005 | Sales Config | Load-wise assumed rates — Sheep430;Goat450 INR/kg | hardcoded | `backend/internal/adminui/app/service.go:4794` | Backend UI-contract literals; verify each frontend consumer. Not same matrix as farm valuation. |
| B006 | Sales Config | Shared sale-readiness report thresholds — 30kg and35kg | hardcoded | `backend/internal/weighing/domain/shed_weights.go:260` | User requires Sales Config ownership; share with Weighing/Growth reporting; do not turn reporting into sale prohibition. |
| B007 | Sales Config | Readiness tolerance bounds — default0g;maximum1000g | hardcoded | `backend/internal/weighing/domain/shed_weights.go:262` | User example200g not current default. Keep report margin distinct from physical error estimate. |
| B008 | Sales Config | Sold-animal weight bands — <20,20–<35,35–<40,>=40kg | hardcoded | `backend/internal/sales/domain/sales.go:550` | Reporting band boundaries; separate from eligibility. |
| B009 | Sales Config | Sale date future window — 60days | hardcoded | `backend/internal/sales/domain/sales.go:50` | Business input window, not technical timeout. |
| B010 | Procurement | Load ageing alert — 90days | hardcoded | `backend/internal/procurement/domain/loadwise.go:217` | Procurement/load-wise ageing policy, not sale price. |
| B011 | Milk | Daily session times — 08:00,12:00,16:00,21:00 | hardcoded | `backend/internal/counts/domain/milk_feeding.go:30` | Published SOP mentions four sessions but code determines exact times. |
| B012 | Milk | Preparation sessions/citric rate — 4sessions;5.5g/L | hardcoded | `backend/internal/counts/domain/milk_preparation.go:18` | Also SQL duplicate at counts/adapters/postgres/milk_preparation.go232–233. |
| B013 | Milk | Stage milk quantities — K1 200ml×4;K2 300ml×4;K3 200ml×sessions1+4 | hardcoded | `backend/internal/counts/domain/milk_preparation.go:155` | Typed stage/session matrix; not a single amount. |
| B014 | Milk | K3 milk window — 7days | hardcoded | `backend/internal/counts/domain/milk_preparation.go:153` | Cohort entry window; not same as stage lookup min_age_days. |
| B015 | Milk | Watchlist graduation — 2consecutive accepted Yes | hardcoded | `backend/internal/counts/domain/milk_feeding.go:164` | Requires accepted evidence semantics, not click-count. |
| B016 | Counts / newborn | Legacy colostrum slots — 07:00,11:00,15:00,18:30,22:00;15min advance | hardcoded | `backend/internal/tasks/domain/templates.go:187` | Legacy fallback; published follow_up may supersede. Do not claim effective without template resolution. |
| B017 | Feed | Packing variance tolerance — 0.2kg | hardcoded | `backend/internal/feeddirection/domain/analytics.go:354` | Analytics threshold distinct from confirmation tolerance. |
| B018 | Feed | Packing confirmation tolerance — 0.5kg | hardcoded | `backend/internal/feeddirection/domain/packing_entry_confirm.go:22` | Different decision point from0.2kg report. |
| B019 | Feed | Low-stock report threshold — 5days | hardcoded | `backend/internal/feeddirection/domain/analytics.go:712` | New Alerts separately configurable source; STG Alerts tables absent. |
| B020 | Feed | Stock notification/forecast — 7days notify;7days forecast | hardcoded | `backend/internal/feeddirection/domain/analytics.go:717` | Different from5day redcard. |
| B021 | Health | Course default/max days — 3default;90max | hardcoded | `backend/internal/health/domain/types.go:13` | Draft default versus validation cap versus actual published duration. |
| B022 | Health | Periparturient window — 14days after kidding | hardcoded | `backend/internal/health/domain/animal_resolution.go:29` | Clinical derived finding, not editable treatment row. |
| B023 | Health | Temperature finding boundaries — Adult fever>103.5F;kid>=103.5F;high>106F;low<100F | hardcoded | `backend/internal/health/diagnosis/form.go:545` | Clinical rules; preserve exact comparator and class distinction. |
| B024 | Health | Kid warm test — >100F | hardcoded | `backend/internal/health/diagnosis/kids.go:84` | Clinical logic, not a universal safety recommendation. |
| B025 | Health | Housing visit cadence — ICU morning+evening;ward/quarantine morning;field no daily cycle | hardcoded | `backend/internal/health/domain/course_schedule.go:65` | Housing owns cadence, protocol owns visit content. |
| B026 | Health | Shift windows — 06:00–15:00 and15:00–24:00;none00:00–06:00 | hardcoded | `backend/internal/health/diagnosis/engine.go:701` | Source hardcoded clinical operations; not all treatment timestamps read from DB grid. |
| B027 | Health | Diagnosis registers — adult-1,kid-milk-7,kid-weaning-1,kid-fattening-1 YAML | hardcoded | `backend/internal/health/diagnosis/embed.go:9` | Committed and embedded; DB Health Config treatment protocols do NOT make diagnostic rules editable. |
| B028 | Health | Emergency precedence — finding-triggered before scope;Director notified after immediate emergency | hardcoded | `backend/internal/health/diagnosis/emergency.go:3` | Clinical execution ordering, not blanket preapproval. |
| B029 | PC | Category proof slots — Deworm/antiprotozoan/ticks1video;trimming3videos;during10s hint | hardcoded | `backend/internal/pccare/domain/domain.go:183` | 10seconds is a hint, not asserted enforcement. Separate category inventory proof. |
| B030 | Toxin | Canonical test sequence — 7steps;5g sample;3min shake;60min settle;3min well;8min strip;readwithin1min | hardcoded | `backend/internal/toxin/domain/task.go:122` | Code-owned procedure.60/3/8minute server gates; prose values and enforced timers differ. |
| B031 | Vaccination reminders | Reminder ladder — D-7 08:00;D-6..0 08:00,13:00,20:30 | hardcoded | `backend/internal/calendar/domain/reminder_cadence.go:55` | Notification policy, not vaccination due-date rule. |
| B032 | Vaccination reminders | Quiet hours — 21:00–07:00 IST | hardcoded | `backend/internal/calendar/domain/reminder_cadence.go:64` | Default code schedule; override/effective path requires config resolver. |
| B033 | Vaccination | Publish/preview buffer fallback — 7days | hardcoded | `backend/internal/protocol/app/publish.go:118` | DB capacity also7 currently; fallback is not duplicate independent setting. |
| B034 | Verification | Default sampling — 100percent | hardcoded | `backend/internal/verification/domain/sampling.go:23` | Person-specific sampling can exist; default is not every person effective rate. |
| B035 | Verification | Review SLA catalogue — 24hours for milk/counts/PC categories | hardcoded | `backend/internal/verificationcatalog/catalog.go:126` | Code-owned category metadata; inspect catalogue for per-category differences. |
| B036 | Workforce | Max leave duration — 90days | hardcoded | `backend/internal/workforce/domain/leave_types.go:27` | Business guard; DB leave-approval config currently0rows. |
| B037 | Market | Backfill allowance — 7days | hardcoded | `backend/internal/market/app/service.go:24` | Business write date window. |
| B038 | Leadership tasks | Near-deadline band — 2days | hardcoded | `backend/internal/leadershiptasks/domain/deadline.go:46` | Business prioritization band. |
| B039 | Alerts | Headcount-change rule — default1,min1,max1000animals | hardcoded | `backend/internal/alerts/domain/rules.go:68` | Source320+ configurable override; not deployed STG317. |
| B040 | Alerts | Low-stock rule — default5,min1,max90days | hardcoded | `backend/internal/alerts/domain/rules.go:79` | Source UI editable and SQL persisted after migration; absent live tables. |
| B041 | Alerts | Feed moved detection epsilon — 0.05kg | hardcoded | `backend/internal/alerts/domain/pen_feed.go:124` | Distinct from configured headcount threshold. |
| B042 | Alerts | Critical lowstock severity — <2days | hardcoded | `backend/internal/alerts/domain/feed_stock.go:30` | Severity logic separate from alert on/off threshold. |
| B043 | Herd Signals | RSSI thresholds — -65/-75/-80 | hardcoded | `backend/internal/herdsignals/domain/types.go:85` | Provisional source defaults, not approved medical signal. |
| B044 | Herd Signals | Battery thresholds — 3000/2800/2600mV;fall80mV;trend30days/min24hours | hardcoded | `backend/internal/herdsignals/domain/types.go:88` | Operational provisional thresholds. |
| B045 | Herd Signals | Motion thresholds — active100,low10,quiet1;window60seconds;spike2.5× | hardcoded | `backend/internal/herdsignals/domain/types.go:94` | Analytics threshold, not hardware sample calibration. |
| B046 | Herd Signals | Status durations — stale/gap/missing30min;quiet90min;inactive180min | hardcoded | `backend/internal/herdsignals/domain/types.go:97` | Business monitoring status policy. |
| B047 | Weighing | Proof authoring limits — max5shed proofs;max8removal slots | domain invariant | `backend/internal/weighing/domain/types.go:638` | Structural bound; actual requested slots are published SOP data. |
| B048 | Weighing | Weight validity — 0.001..100000kg | domain invariant | `backend/internal/weighing/domain/weight_correction.go:116` | Domain guard, not a sensible animal target/eligibility weight. |
| B049 | Feed | Captured packed/waste mass cap — 10000kg | domain invariant | `backend/internal/feeddirection/adapters/postgres/packing_completions.go:745` | Validation bound; not packing target. |
| B050 | SOP | Follow-up series bound — 100rounds | domain invariant | `backend/internal/tasks/domain/sop_followup.go:155` | Structural bound separate from authored schedule count. |
| B051 | Procurement | Inspection authoring cap — 200questions;max_files0..10 | domain invariant | `backend/internal/animalpurchase/domain/inspection.go:166` | Schema guard; current47served questions not200default. |
| B052 | Identity | Global unique identifiers/no autolink — identifier1/2,temp,BLE;reject bad/missing scope | domain invariant | `backend/internal/identity/domain/types.go:1` | Exact active DB policy in feature-config-value-readback; source entry only. Not freely editable generic config. |
| B053 | Feed | Catalogue / ration groups / dated rates — 17items/4active;16breed mappings;g/head×park/group/stage/date | DB-seed/editable | `research/feature-config-value-audit.md:1` | Existing Feed Config; null nutrition factors, zero quantities and retired historical IDs retained. |
| B054 | Feed | Normal/experiment schedules — 07:00 or14:00 direction;14:00 correction;15:30 transport | DB-seed/editable | `research/feature-config-value-audit.md:1` | Actual open-ended rows from2026-08-07; perpark/workflow. |
| B055 | Feed | Experiment ration values — grams_per_head by exact feed/pen/group;warmup20:80 appears in labels | DB-seed/editable | `research/feature-config-value-audit.md:1` | Label is not authoritative global blend ratio. |
| B056 | Vaccination | Capacity — 200/day tenant;buffer7;table maxshots3 | DB-seed/editable | `research/feature-config-value-audit.md:1` | Publishedv9drivepolicy says2;effective precedence unresolved. |
| B057 | Vaccination | Protocol plan — published9/draft10;clinical matrices,age16weeks,wave28days,warmup7 | DB-seed/editable | `research/feature-config-value-audit.md:1` | Specialized editor, not generic scalar store. Full actual DSL in feature-config-documents.json. |
| B058 | Vaccination | Clinical gaps — live/live28;killed combinations14;booster21;pregnancymonth4–5;catchup14 | DB-seed/editable | `research/feature-config-value-audit.md:1` | Observed product rules, not independent advice. Preserve complete conditions/overrides. |
| B059 | Vaccination | Plan versus SOP proof — plan shed_level_video;drive SOP per_goat_video | unverified effective precedence | `research/feature-config-value-audit.md:1` | Both persisted; trace resolver rather than silently choosing. |
| B060 | Health | Treatment protocols — 54published versions/27diseasekeys/1012steps | DB-seed/editable | `research/feature-config-value-audit.md:1` | Dose/route/duration/critical-action authored; NOT embedded diagnosis rules. |
| B061 | Sales | Market benchmarks — 10rows;370/kg market;exfarm/transport text;some NULL | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | DB data observed; exact editing workflow not established in this audit; not valuation default. |
| B062 | Procurement / Sales | Vendor dropdown vocabulary — breed/feed/capacity/status/frequency/record types | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | Separate catalogue identity; observed rows, precise authoring control unverified. |
| B063 | Inventory | Vaccine item catalogue — 7active,doseunit | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | Master records do not create dosing rules or stock quantities. |
| B064 | Identity / Counts | Stage age bands — K0 0–1,K1 2–7,K2 8–42,K3 43+;19rows | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | Actual lookup differs from milk-specificwindow; not automatically contradictory. |
| B065 | Identity | Identifier policy — phase1-identifier-v1;global uniqueness;autolinkfalse | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | Primaryallowed first/temp only; no editUI confirmed. |
| B066 | Workforce | Leave approval configuration — 0observed rows | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | Do not invent effective default; service fallback unresolved. |
| B067 | Workforce / Vaccination | Assignment/shift configuration — 1and3operators;08:30–18,07–15,15–23:59,08–18 | DB-seed/editable | `research/feature-config-value-audit.md:1` | Source authoring exists; individual employees omitted. |
| B068 | Locations | Operational-use/holding flags — 176rows;1holding;allobservedusable | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | Not same as animalstage labels or permissions. |
| B069 | Counts / Milk / Feed / Weighing | Published SOP form/proof/compatibility — 12published SOP identities;perfeatureproofcounts/approval/rework | DB-seed/editable | `research/feature-config-value-audit.md:1` | Fulldocuments captured; code hooks and legacy defaults still matter. |
| B070 | Weighing / Feed | Feed-water cutoff — 21:00 | DB-seed/editable | `research/feature-config-value-audit.md:1` | ActualDBvalue; do not retain earlier hardcoded cutoff claim. |
| B071 | Market | Call time — 08:00 | DB-seed/editable | `research/feature-config-value-audit.md:1` | ActualDBvalue and existingUI. |
| B072 | UI contract | Presentation entries — 0rows | DB-stored/UI-unverified | `research/feature-config-value-audit.md:1` | Not business rule repository. |

## Database guard and seed audit

The actual live schema guard/default query is saved in business-db-constraints-queries.sql, with read-only readback in business-db-constraints-readback.txt. It reads PostgreSQL constraint definitions and column defaults rather than assuming old migrations equal the deployed database. Numeric positivity/order checks are invariants, not automatically configurable business policy. Examples in source: baseline migration `backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql:2053` stage max>=min; `:3804` procurement warm-up nonnegative; `:4009–4013` protocol gap/offset/window nonnegative and enumerated trigger/repeat choices; `:4504–4505` status axes and positive expected durations; `:4582–4585` capacity buffer nonnegative and scope vocabulary; `:13508–13510` feed energy nonnegative and wastage0<=x<1. These are historical declaration pointers; the fresh live receipt is authoritative for deployed guards.

Do not offer “turn off uniqueness,” negative ration, arbitrary unknown rule operator or overwrite clinical history as ordinary business configuration. Similarly, category enums often dispatch a specific engine; adding a label does not implement a new category's behavior. A schema default and a persisted value may differ. The readback contains actual defaults, while feature-config-value-readback.txt and feature-config-extra-readback.txt contain actual values/documents.

## Embedded clinical rule coverage that must not be missed

Four committed YAML registers are embedded by `health/diagnosis/embed.go:21–31`. All2,108 non-comment YAML lines are indexed with path and line in business-embedded-rules-index.json, including rule IDs, clauses, scores, duration/exit types and treatment/housing/priority directives. They are source-managed clinical configuration, separate from the54 DB treatment-protocol versions. This index preserves concrete source rules without asserting a veterinary safety review. A future editor needs clinical ownership, validation, versioning and engine compatibility; exposing arbitrary JSON is not equivalent.

## Sweep scope and unresolved candidates

The candidate sweep enumerated current backend/internal and backend/migrations/postgres trees, excluded generated sqlc and *_test.go, and searched Go/SQL/JSON business-keyword/numeric lines. It produced7,965 candidate lines in business-literal-candidates.json; these include technical/math/comment/fixture noise and are NOT7,965 confirmed settings. A separate four-file YAML index closes the embedded-rule gap. The earlier65-domain inventory provides family coverage; this report's72 curated entries are grouped settings, not a claim of exhaustively interpreted952 Go files. JSON evidence identifies revision and source lines.

Priority remaining resolver work: published follow_up versus legacy templates; vaccination plan maxshots2 versus capacity3 and plan-shed/SOP-per-animal proof; person verification sampling versus100% default; workforce leave fallback with0 configured rows; clinical code interpretation versus authored protocol content; exact current authoring control for some lookup tables. These are explicitly unverified, not silently hardcoded or editable. Current source through326 differs from live applied317. No live Alerts rows were observed.

Potential configuration candidates should be ranked by business ownership and meaningful user change, not by replacing every number. First Sales-owned matrices/shared reporting; then feed/milk schedules and ration thresholds; then domain-approved clinical authoring and evidence policies; then notifications/operational monitoring. Integrity bounds remain enforced even when a business value becomes editable.

No implementation edits, database writes, migration, push, merge or deployment performed.

## Resolved follow-up

vaccination-policy-precedence.md traces the capacity override and task-SOP proof selection. Capacity3 overrides DSL2 in the current kernel sweep. A separate hardcoded combo-product limit3 remains in obligation/app/combo_session.go:9. Task-pinned SOP policy controls backend/Android execution proof; the raw protocol policy is not automatically effective. Earlier “unverified precedence” labels are historical pending statements superseded by this source trace, with per-task deployed-state verification still bounded.

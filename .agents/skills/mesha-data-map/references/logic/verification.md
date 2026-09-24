Verification logic card: /verify status legend (pending/approved/rejected), missed flag, queue filters (module/park/pen/date), oversight KPIs (videos waiting, oldest pending, age buckets, review speed, est days to clear, reject rate 30d, median latency per module, pending by module, 14-day volume, verifier activity 14d), randomization (captured/selected/reviewed/auto-accepted per category), video log, verifier integrity, weighing verification status.

# Verification queue (/verify; /verification redirects to /verify)

Source table for everything: `public.verification_items` (one row per proof item; `status` in
pending | approved | rejected | withdrawn; `auto_resolution` NULL = decided by a person,
`'not_sampled'` = auto-approved because randomization skipped it). All values below: goatos-stg, as of 24/09/2026 (IST), 1 tenant.

## Vocabulary
- **approved** = "accepted"/"verified". **rejected** = "rework" (weighing view calls it `rework`). No separate rework/reshot status: a reshoot is a NEW item; the old rejected row stays rejected (history). No `submitted_at` column; time is `captured_at` (capture date, IST business day) and `verified_at` (verdict time).
- **withdrawn** = producer superseded the source work (`WithdrawItemsBySource`, repository.go:2862; only pending items). Excluded from every screen count and from views' `total`.
- **Sampling** (samplingsql/samplingsql.go:34 `InSample`): item is in sample if `sampling_bucket < sample_percent` of the latest `verification_sampling_policies` row for its category with `effective_business_date <= capture day` (default 100). Unsampled pending items are later auto-approved with `auto_resolution='not_sampled'` (adapters/postgres/sampling.go:300). Policies now: feed_distribution 75% (from 2026-09-11), vaccination_proof 25% (from 2026-09-05).
- **Module mapping**: `vertical` (area in ceo_ai view), `module` (source module), `category` (page). Nav module (screen chip) = verificationcatalog/catalog.go definitions (e.g. Feed = feed_distribution+packing+transport+wastage; Counts = shifting_move+pen_reconciliation+births/deaths; Weighing = weighing_proof+weighing_fasting; Health = health_adults/kids). Duty-code -> nav key: module_key_translation.go:17 (`preventive_care`->vaccination, `feed.direction`->feed_direction, strip `pc.`, dots->underscores). Trap: vertical != module (module weighing has vertical farm_ops/operations for 7 items).
- **Pen vs partition**: shed filter key = `shed_id#partition`, partition predicate `regexp_replace(lower(coalesce(nullif(btrim(partition_label),''),'whole')),'^part\s+','')` (adapters/postgres/shed_filter.go:49). 'whole' = whole-shed item. ceo_ai views group by shed only (partitions merged).

## 1. Status legend pills (pending / approved / rejected counts)
- Endpoint: `GET /verification/queue` -> `filter_options.counts` (handler.go:63; service.go:131; SQL repository.go:654).
- Formula: `count(*) GROUP BY status` over status IN (pending,approved,rejected), same filters as the table minus status, no pagination. Sampling predicate applied only for callers WITHOUT `verification.oversee` (verifier); CEO/Director see all items.
- Screen default = TODAY's capture day (page lands on today; `business_date` -> `captured_at` in [day 00:00 IST, next day)). Range picker -> `business_date_from/to` inclusive. Missed toggle -> pending captured before today.
- Filters -> SQL: nav_module -> `category = ANY(module categories)`; category -> `category`; park -> `park_id`; shed -> `shed_id` + partition predicate; status tab -> `status` (tab `all` = none; default pending).
- STG capture day 24/09 (re-run 25/09 00:40 IST, after the 00:00 IST sweep auto-approved unsampled items): pending 35, approved 425, rejected 0 (sum 460 = arrivals that day; before the sweep 78/382).
```sql
SELECT status, count(*) FROM public.verification_items
WHERE status IN ('pending','approved','rejected')
  AND captured_at >= '2026-09-24'::timestamp AT TIME ZONE 'Asia/Kolkata'
  AND captured_at <  '2026-09-25'::timestamp AT TIME ZONE 'Asia/Kolkata'
GROUP BY 1;   -- public.* needed: ceo_ai views have no capture date
```
- All-time (no date): `SELECT sum(pending),sum(accepted),sum(rejected),sum(withdrawn) FROM ceo_ai.verification_queue_status` -> 3142 / 19278 / 138 / 188 (25/09). Trap: accepted 19278 includes 713 auto-approved `not_sampled` (person-decided approvals 18565); view cannot separate them.
- Missed (pending captured before today, "has_missed" flag repository.go:697): 3107.

## 2. Queue table rows
- Same endpoint, repository.go:376. Keyset page of 20, `ORDER BY captured_at, item_id` (asc default, toggle desc). "In queue" age = now - captured_at, pending only (amber >4h, red >7d). Never count rows from a page; use the legend aggregate.
- Verifier lens (`verification.review`, no oversight): no date clamp, full pending backlog, sampled.

## 3. Oversight analytics (CEO / PC Director; `GET /verification/oversight-analytics`, handler.go:72; SQL adapters/postgres/oversight_analytics.go). Whole tenant, no filters, cached.
| Metric | Line | Formula | STG 24/09 |
|---|---|---|---|
| Videos waiting | :40 | pending AND in-sample | 3142 |
| Oldest pending (h) | :40 | max(now-captured_at) of same set | 1213.6 h (~50.6 d; capture 05/08/2026 10:09 IST, CBE weighing_proof) |
| Age buckets | :40 | <=1d / 1-3d / 3-7d / >7d by now-captured_at | 35 / 23 / 488 / 2596 |
| Review speed (verdicts/active day, 7d) | :64 | person verdicts (auto_resolution NULL) last 7x24h / distinct IST days with verdicts | 2635/7 = 376.4 |
| Est days to clear | derived | videos_waiting / review speed | 3142/376.4 = 8.3 d |
| Reject rate 30d | :111 | rejected/(approved+rejected), person verdicts, verified_at last 30d | 80/12464 = 0.64% |
| Median review latency per module (30d) | :81 | median(verified_at-captured_at) h, person verdicts | feed 1.7, weighing 2.2, milk_feeding 10.8, counts 15.2, pen_visits 17.8, milk_preparation 20.8, pc_care 73.8, vaccination 175.0 |
| Pending by module | :126 | pending AND in-sample GROUP BY module | feed 2802, weighing 175, milk_feeding 50, milk_preparation 34, counts 33, pen_visits 26, pc_care 22 |
| Daily volume 14d | :168 | per IST day: verdicts (person) by verified_at; arrived (all statuses incl. withdrawn) by captured_at | 24/09: 384 verdicts, 460 arrived; 23/09: 425/443; 22/09: 533/606 |
| Verifier activity 14d | :233, :299 | per verified_by: verdicts/approved/rejected (includes auto rows only if verified_by set), watched-to-end >=90%, verdict without play | Jyothi 5238 (5235 approved, 3 rejected) |
```sql
-- videos waiting / pending by module (sampling-aware; public.* needed)
SELECT vi.module, count(*) FROM public.verification_items vi
WHERE vi.status='pending' AND vi.sampling_bucket < COALESCE((SELECT p.sample_percent
  FROM public.verification_sampling_policies p WHERE p.tenant_id=vi.tenant_id AND p.category=vi.category
  AND p.effective_business_date <= (vi.captured_at AT TIME ZONE 'Asia/Kolkata')::date
  ORDER BY p.effective_business_date DESC LIMIT 1),100)
GROUP BY ROLLUP(1);
-- reject rate 30d
SELECT count(*) FILTER (WHERE status='rejected')::numeric/nullif(count(*),0)
FROM public.verification_items WHERE verified_at >= now()-interval '30 days' AND auto_resolution IS NULL;
```
Trap: ceo_ai.verification_queue_status pending ignores sampling, so it can exceed "videos waiting" (24/09 evening 3185 vs 3142); after each nightly not_sampled sweep they converge (25/09: 3142 = 3142). Leadership legend (unsampled) also uses 3185-style counts.

## 4. Randomization panel (CEO only; `GET /verification/sampling?business_date=`, handler.go:74; SQL adapters/postgres/sampling.go:178)
Per category for one capture day: captured = non-withdrawn; selected = captured AND bucket < percent; reviewed = person verdicts among selected; auto-accepted = `auto_resolution='not_sampled'`. STG 24/09: feed_packing 100% 188/188/188/0; feed_distribution 75% 188/145/145/0; feed_wastage 30/30/30; pen_reconciliation 22/22/0; feed_transport 17/17/17; pen_visit 6/6/0; milk_feeding 5/5/2; milk_preparation 2/2/0; shifting_move 2/2/0. Setting % = `PUT /verification/sampling/{category}`, effective today onward (not a CEO question to answer with SQL). public.* only.

## 5. Video log (`GET /verification/video-log`, handler.go:73; SQL adapters/postgres/video_log.go:70)
One capture day, per shed: items with status <> withdrawn and shed_id set; proof arrival = `proof_artifacts.uploaded_at` per media ref; "not uploaded" = uploaded_at NULL. STG 24/09: 453 items across 18 sheds (7 of 460 arrivals have no shed). public.* only.

## 6. Verifier integrity (ceo_ai.verifier_review_integrity; drill-down /verify analytics)
Per verifier x park x category x IST verdict day: videos_reviewed (approved+rejected with verified_by, includes auto rows if verified_by set), watch_fraction from play spans, below_watch_threshold (<0.9), missing telemetry, reject_rate. Never re-average medians/rates; recompute from counts. Last 30d (>=26/08): reviewed 12464, rejected 80, below 90% watch 1321, no telemetry 1562.
```sql
SELECT sum(videos_reviewed), sum(rejected_count), sum(below_watch_threshold_count), sum(missing_review_telemetry_count)
FROM ceo_ai.verifier_review_integrity WHERE business_day >= DATE '2026-08-26';
```

## 7. Weighing verification (ceo_ai.weighing_verification_status; filters `module='weighing'`)
pending 175, rework (=rejected) 17, verified 2563, withdrawn 101. Differs from queue view area 'weighing' (pending 168) because that groups by `vertical`. Use this view for "weighing videos pending".
```sql
SELECT sum(pending), sum(rework), sum(verified), sum(withdrawn), min(oldest_pending_at) FROM ceo_ai.weighing_verification_status;
```

## Per-area now (ceo_ai.verification_queue_status, pending / accepted / rejected / withdrawn)
feed 2802/15352/121/73 (25/09); weighing 168/2559/17/101; counts 117/114/0/0; preventive_care 48/1221/0/14; farm_ops 5/0/0/0; operations 2/14/0/0; growth 0/4; livestock 0/14.
```sql
SELECT area, sum(pending), sum(accepted), sum(rejected), sum(withdrawn), min(oldest_pending_at)
FROM ceo_ai.verification_queue_status GROUP BY 1 ORDER BY 2 DESC;
```

## Traps
- Screen lands on TODAY; ceo_ai views are all-time current state. Say which one you answer.
- "Pending" three ways: view/leadership legend (all pending, ignores sampling), videos waiting (sampled), verifier queue = sampled. 25/09 after the nightly sweep all three = 3142; between sweeps the view is higher (24/09: 3185).
- Accepted includes auto `not_sampled` approvals; for "how many did the verifier approve" use `auto_resolution IS NULL`.
- Rejected never turns into approved: a reshoot is a new item; don't subtract.
- Withdrawn excluded from totals; `total_including_withdrawn` exists in views.
- Vaccination proofs are closed per drive batch; vaccination_proof pending is 0 now. The 48 preventive_care pending = pc_care 22 (deworming 10, feed/water removal 6, hoof trimming 6) + pen_visit 26.
- Toxin review (/verify?toxin=1) is NOT verification_items; it is `/toxin/review`.
- Rolling windows (7d/14d/30d) are `now() - interval`, not IST day boundaries (except daily volume).

## CEO questions this card answers
- How many videos are pending verification? / Kitne videos verify hona baaki hai?
- What's the oldest unverified video? / Sabse purana pending video kitna purana hai?
- How fast is the verifier clearing, and how many days to clear the backlog? / Backlog kitne din mein clear hoga?
- What is the reject rate this month? / Is mahine kitne reject hue?
- Which module has the most pending? / Kis module mein sabse zyada pending hai?
- How many came in today and how many were verified today? / Aaj kitne aaye, kitne verify hue?
- Is the verifier actually watching the videos? / Verifier poora video dekh raha hai ya nahi?
- Weighing videos pending / rework? / Weighing ke kitne videos pending ya rework mein hain?
- What sampling % is set and how many were auto-accepted? / Randomization kitna hai, kitne auto-accept hue?
- Which pens sent videos today? / Aaj kin pens se video aaye?

## 8. Review drawer and toxin list (/verify)
- Drawer: `features/verification-review/verification-review-drawer.tsx:590-720` shows the queue row's fields (module, park/pen/partition, captured_at, status, proof media) plus Approve/Reject (`POST /verification/items/{item_id}/verdict`, handler.go:66). No separate endpoint; values = section 2 row. Play/pause/seek events go to `POST /verification/review-events` (handler.go:70) -> `verification_review_events`; watch-fraction logic is Go-only (`adapters/postgres/review_events.go:237 computeActorFacts`: per-proof play->pause/ended spans, merged, / max video_duration_ms) and feeds section 6.
- Toxin list (`?toxin=1`, `toxin-review-list.tsx:111-116`: Test, Reading, Status) = `GET /toxin/review`; counts in approvals.md A6 (pending_review 6 on 24/09).
- Questions: "Did the verifier watch this video fully?" / "Is video ko poora dekha gaya?"

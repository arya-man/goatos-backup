# goatos-stg DB-wide audit (2026-09-24)
Target: goatos-stg:asia-south1:goatos-stg-core-db (Postgres 16.14), role mesha_ceo_readonly, all queries in BEGIN READ ONLY / ROLLBACK with default_transaction_read_only=on. Stats reset 2026-07-10 (~76 days of counters). Proxy stopped, temp creds deleted.

## Instance / settings
- Tier db-g1-small (shared core, ~1.7 GB RAM), ZONAL, 20 GB PD_SSD with autoresize. DB size 8.0 GB across 343 tables (app_events alone 2.6 GB).
- shared_buffers 128 MB, effective_cache_size ~788 MB, work_mem 4 MB, maintenance_work_mem 64 MB, random_page_cost 4 (SSD: should be ~1.1), max_connections 50 (28 open at audit time), autovacuum_vacuum_scale_factor 0.2, track_io_timing off.
- pg_stat_statements: NOT installed (extensions: plpgsql, btree_gist, pgcrypto, pg_trgm, dblink). Query Insights: OFF (no insightsConfig).
  - To enable (not done): `gcloud sql instances patch goatos-stg-core-db --project goatos-stg --insights-config-query-insights-enabled --insights-config-record-application-tags --insights-config-record-client-address`; pg_stat_statements: `CREATE EXTENSION pg_stat_statements;` as cloudsqlsuperuser (Cloud SQL preloads it). random_page_cost via `--database-flags=random_page_cost=1.1` (note: patching flags replaces the full flag list, keep cloudsql.iam_authentication=on).

## analytics.app_events (2.6 GB: heap 1871 MB, idx 706 MB, toast 23 MB)
- 1,576,909 rows, oldest 2026-08-13 (the day migration 000153 created the table), newest today. So the entire table is ~42 days.
- ~1.2 KB/row heap: properties jsonb avg 651 B + client_info jsonb avg 247 B + uuid/text columns; 6 indexes (pkey, device+received, received, tenant+client_event_id uniq, tenant+event+received, tenant+coalesce(time)).
- Volume: Aug 361k, Sep 1.22M. Daily 35k-60k typical, peaks 100k (Sep 11-12). Only 68 devices in the last 7 days -> ~700 events/device/day.
- Top events: route_entered 100k (6.3%), pc_care_slot_proof_preview 76k, sync_write_enqueued 68k, sync_write_attempt_started 60k, sync_write_succeeded 57k, route_exited_via_back 44k, proof_upload_started/registered, proof_processing_started/completed, proof_gallery_save_started/completed (~36-39k each), api_call_failure 36k. Long flat tail -> telemetry-style lifecycle events (started/succeeded pairs) dominate.
- Pruning: none. Only DELETE in code is in a test (feed_direction_oci_integration_test.go). cmd/analytics-rollup rolls up into *_daily tables but does not delete raw rows. Growth ~50 MB/day at current rate -> ~1.5 GB/month on a 20 GB disk.
- Also: 548 seq scans averaging 968k rows each (530M rows read) - something scans the whole table.
- Fix: retention job (e.g. 30-60d raw, rely on daily rollups), partition by received_at month for cheap drops, sample/drop the started/succeeded pairs client-side.

## Raw results (sections 1-6)
```
BEGIN
## 1 TABLE SIZES
                     tbl                     | est_rows |  total   |  heap   |   idx   |   toast    
---------------------------------------------+----------+----------+---------+---------+------------
 analytics.app_events                        |  1528232 | 2600 MB  | 1871 MB | 706 MB  | 23 MB
 public.outbox_messages                      |   359087 | 962 MB   | 773 MB  | 188 MB  | 552 kB
 public.audit_log                            |   440634 | 831 MB   | 245 MB  | 586 MB  | 368 kB
 public.notification_requests                |   233337 | 628 MB   | 427 MB  | 201 MB  | 528 kB
 public.herd_signal_activity_windows         |   748714 | 277 MB   | 114 MB  | 163 MB  | 8192 bytes
 public.domain_event_processed_events        |    89414 | 133 MB   | 43 MB   | 90 MB   | 8192 bytes
 public.obligation_instances                 |    86616 | 105 MB   | 29 MB   | 76 MB   | 8192 bytes
 public.proof_artifacts                      |    45179 | 100 MB   | 50 MB   | 50 MB   | 8192 bytes
 public.feed_direction_issue_rows            |    86182 | 93 MB    | 27 MB   | 66 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_19      |   133422 | 92 MB    | 44 MB   | 47 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_17      |   134409 | 91 MB    | 44 MB   | 47 MB   | 8192 bytes
 public.notification_delivery_attempts       |   238737 | 89 MB    | 44 MB   | 44 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_14      |   128514 | 88 MB    | 43 MB   | 45 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_15      |   127825 | 87 MB    | 42 MB   | 44 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_20      |   129746 | 85 MB    | 41 MB   | 44 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_16      |   130084 | 85 MB    | 41 MB   | 44 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_12      |   124000 | 84 MB    | 41 MB   | 43 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_18      |   126618 | 83 MB    | 40 MB   | 43 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_13      |   125408 | 82 MB    | 39 MB   | 42 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_30      |   119301 | 80 MB    | 38 MB   | 41 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_24      |   121475 | 78 MB    | 38 MB   | 40 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_31      |   118057 | 76 MB    | 37 MB   | 40 MB   | 8192 bytes
 public.herd_signal_packets_default          |   116427 | 75 MB    | 37 MB   | 38 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_27      |   111831 | 74 MB    | 36 MB   | 38 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_25      |   112956 | 74 MB    | 36 MB   | 38 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_28      |   108535 | 72 MB    | 35 MB   | 37 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_29      |   106077 | 71 MB    | 35 MB   | 37 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_01      |   108885 | 71 MB    | 34 MB   | 37 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_02      |   108534 | 71 MB    | 34 MB   | 37 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_26      |   108593 | 71 MB    | 34 MB   | 36 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_03      |   106939 | 69 MB    | 33 MB   | 35 MB   | 8192 bytes
 public.verification_review_events           |   105681 | 60 MB    | 32 MB   | 28 MB   | 8192 bytes
 public.obligation_status_events             |    96681 | 58 MB    | 27 MB   | 31 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_04      |    77706 | 51 MB    | 25 MB   | 27 MB   | 8192 bytes
 public.herd_signal_packets_p2026_08_23      |    72397 | 49 MB    | 23 MB   | 25 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_11      |    76603 | 49 MB    | 24 MB   | 25 MB   | 8192 bytes
 public.herd_signal_packets_p2026_09_21      |    61890 | 41 MB    | 20 MB   | 21 MB   | 8192 bytes
 public.verification_items                   |    22348 | 39 MB    | 14 MB   | 25 MB   | 112 kB
 public.idempotency_keys                     |    47682 | 37 MB    | 18 MB   | 19 MB   | 8192 bytes
 public.feed_packing_completions             |     8040 | 11 MB    | 7360 kB | 3896 kB | 8192 bytes
 public.feed_distribution_completions        |     8375 | 10176 kB | 7160 kB | 2976 kB | 8192 bytes
 public.weighing_idempotency_records         |     6203 | 8016 kB  | 5384 kB | 2504 kB | 96 kB
 public.vaccination_completions              |     6098 | 7464 kB  | 1808 kB | 5616 kB | 8192 bytes
 public.vaccination_drive_assignment_members |     6329 | 6336 kB  | 1976 kB | 4328 kB | 0 bytes
 public.weighing_observations                |     2852 | 5904 kB  | 1472 kB | 4392 kB | 8192 bytes
 public.goats                                |     1741 | 3944 kB  | 832 kB  | 3072 kB | 8192 bytes
 public.pc_care_task_animals                 |     2181 | 3448 kB  | 2344 kB | 1064 kB | 8192 bytes
 public.goat_identifiers                     |     3453 | 3336 kB  | 880 kB  | 2416 kB | 8192 bytes
 public.goat_identity_events                 |     2318 | 3256 kB  | 1480 kB | 1736 kB | 8192 bytes
 public.feed_packing_verified_quantities     |     8834 | 3136 kB  | 2064 kB | 1032 kB | 8192 bytes
 public.vaccination_source_facts             |     5847 | 3104 kB  | 1712 kB | 1352 kB | 8192 bytes
 public.obligation_batches                   |      776 | 2672 kB  | 808 kB  | 416 kB  | 1416 kB
 public.herd_register_summary_projection     |      144 | 2584 kB  | 704 kB  | 1840 kB | 8192 bytes
 public.protocol_rule_dimensions             |      528 | 2144 kB  | 1696 kB | 408 kB  | 8192 bytes
 public.feed_ration_rates                    |     2576 | 1848 kB  | 616 kB  | 1192 kB | 8192 bytes
 public.sop_task_scan_attempts               |     1574 | 1560 kB  | 552 kB  | 968 kB  | 8192 bytes
 public.herd_register_goat_projection        |     1740 | 1488 kB  | 456 kB  | 992 kB  | 8192 bytes
 public.feed_wastage_completions             |     1134 | 1488 kB  | 824 kB  | 624 kB  | 8192 bytes
 public.procurement_vendors                  |      671 | 1304 kB  | 176 kB  | 1088 kB | 8192 bytes
 public.sop_task_scan_captures               |     1114 | 1240 kB  | 424 kB  | 776 kB  | 8192 bytes

 db_size | n_tables 
---------+----------
 8010 MB |      343

## 2 STAT USER TABLES by seq_tup_read
                     tbl                     | seq_scan | seq_tup_read | avg_rows_per_seq | idx_scan  | n_live_tup | n_dead_tup |     av     |     aa     |     la     
---------------------------------------------+----------+--------------+------------------+-----------+------------+------------+------------+------------+------------
 public.obligation_instances                 |  1362213 |  14044583027 |            10310 | 341441691 |      86684 |       5636 | 2026-09-05 | 2026-09-22 | 2026-07-25
 public.goats                                |  6294435 |  10291084408 |             1634 | 179787239 |       1741 |         20 | 2026-09-22 | 2026-09-22 | 2026-07-25
 public.weighing_observations                |  1363788 |   2304241600 |             1689 |   1270169 |       2863 |        313 | 2026-09-22 | 2026-09-22 | 
 public.feed_direction_issue_rows            |    48705 |   2058574606 |            42266 |    481165 |      86182 |         40 | 2026-09-22 | 2026-09-24 | 
 public.obligation_batches                   |  9316666 |   1416288227 |              152 |  92854978 |        777 |         90 | 2026-09-22 | 2026-09-22 | 
 public.goat_shed_partitions                 |   811830 |   1356144601 |             1670 | 117357748 |       1740 |        187 | 2026-09-07 | 2026-09-18 | 
 public.vaccination_drive_assignment_members |   113979 |    661386489 |             5802 | 750500770 |       6328 |        947 | 2026-09-22 | 2026-09-22 | 
 public.locations                            |  4645438 |    560751281 |              120 | 560295709 |        179 |         53 | 2026-08-10 | 2026-08-10 | 2026-07-25
 public.goat_identifiers                     |   178190 |    541076033 |             3036 |  14506236 |       3455 |        620 | 2026-08-04 | 2026-09-20 | 2026-07-25
 analytics.app_events                        |      548 |    530588293 |           968226 |   1534914 |    1579803 |        463 | 2026-09-22 | 2026-09-23 | 
 public.workforce_members                    | 12361415 |    394778649 |               31 |   5817728 |         42 |         16 | 2026-09-10 | 2026-09-08 | 2026-07-25
 public.vaccination_drive_assignments        |  1300576 |    293134683 |              225 | 165417332 |        740 |        139 | 2026-09-22 | 2026-09-22 | 
 public.vaccination_completions              |    51740 |    291597995 |             5635 | 194467396 |       6214 |        873 | 2026-08-22 | 2026-09-11 | 2026-07-25
 public.protocol_rules                       |  6650957 |    268353464 |               40 | 126192136 |        209 |          5 | 2026-09-18 | 2026-09-01 | 2026-07-25
 public.user_scope_grants                    |  3952993 |    247063401 |               62 |    406189 |         73 |          2 | 2026-09-23 | 2026-08-14 | 
 public.protocol_versions                    | 34761656 |    237794416 |                6 |   2958231 |         10 |         26 |            |            | 2026-07-25
 public.notification_requests                |    11737 |    196126250 |            16710 |   2493295 |     238710 |      30754 | 2026-09-21 | 2026-09-22 | 
 public.weighing_shed_observations           |  2361352 |    180849057 |               76 |    169772 |        108 |         19 | 2026-09-14 | 2026-09-14 | 
 public.person_access                        |  3671876 |    137781831 |               37 |    279396 |         40 |         25 |            | 2026-09-02 | 
 public.verification_review_events           |     3530 |    136866497 |            38772 |   1247395 |     112033 |         72 | 2026-09-17 | 2026-09-22 | 
 public.outbox_messages                      |     1260 |    112262553 |            89097 |   1643723 |     362562 |      57667 | 2026-09-19 | 2026-09-23 | 
 public.verification_items                   |    19363 |    108445869 |             5600 |   2592111 |      22564 |       3960 | 2026-09-16 | 2026-09-24 | 
 public.sop_task_scan_captures               |   380369 |     87454695 |              229 |  22554806 |       1237 |         81 | 2026-08-13 | 2026-09-04 | 
 public.animal_stage_lookup                  |  6761439 |     83151245 |               12 |   6985024 |         19 |         30 |            | 2026-08-06 | 
 public.audit_log                            |      554 |     72792506 |           131394 |      2231 |     471789 |         63 | 2026-09-20 | 2026-09-21 | 
 public.pc_care_task_assignees               |   457561 |     62910703 |              137 |     35637 |        311 |          2 |            | 2026-09-13 | 
 public.shed_partitions                      |   441069 |     56400088 |              127 |   1380535 |        130 |          1 | 2026-09-22 | 2026-09-22 | 
 public.proof_artifacts                      |     6945 |     52218191 |             7518 |  35685372 |      45821 |       1619 | 2026-09-22 | 2026-09-23 | 
 public.herd_signal_activity_windows         |     1221 |     42109020 |            34487 |  16773138 |     749170 |      65646 | 2026-09-21 | 2026-09-24 | 
 public.notification_delivery_attempts       |      338 |     41397972 |           122479 |    242152 |     241102 |         14 | 2026-09-21 | 2026-09-23 | 
 public.protocol_definitions                 | 40614258 |     40614254 |                0 |     44688 |          1 |          4 |            |            | 2026-07-25
 public.obligation_status_events             |    18099 |     34564802 |             1909 |   2706069 |      99298 |        683 | 2026-09-01 | 2026-09-04 | 2026-07-25
 public.weighing_campaign_sheds              |   183013 |     33452087 |              182 |   1044115 |        378 |        107 | 2026-09-21 | 2026-09-22 | 
 public.pc_care_tasks                        |   282112 |     28417817 |              100 |   1337195 |        287 |         39 | 2026-09-22 | 2026-09-22 | 
 public.domain_event_processed_events        |      311 |     26147646 |            84076 |   1347488 |      87761 |      14370 | 2026-09-23 | 2026-09-23 | 
 public.sop_submissions                      |  1118090 |     25532049 |               22 |  24137877 |         82 |          8 | 2026-08-28 | 2026-08-12 | 
 public.pc_care_task_animals                 |    96934 |     19000745 |              196 |    748374 |       2181 |          0 | 2026-09-22 | 2026-09-22 | 
 public.person_park_scope                    |   677180 |     18971150 |               28 |   1400871 |         29 |         21 |            | 2026-09-11 | 
 public.weighing_work_items                  |   243816 |     18448465 |               75 |    309198 |        312 |         93 | 2026-09-14 | 2026-09-21 | 
 public.workforce_positions                  |   816512 |     16093412 |               19 |       304 |         23 |          4 |            |            | 2026-07-25

          stats_reset          
-------------------------------
 2026-07-10 13:38:28.687405+00

## 3a UNUSED INDEXES
                    tbl                    |                       idx                       |   sz    | uniq | pk 
-------------------------------------------+-------------------------------------------------+---------+------+----
 public.audit_log                          | audit_log_tenant_resource_recorded_idx          | 56 MB   | f    | f
 public.audit_log                          | audit_log_tenant_actor_recorded_idx             | 43 MB   | f    | f
 public.notification_requests              | notification_requests_feed_alerts_idx           | 34 MB   | f    | f
 public.feed_packing_completions           | feed_packing_completions_idempotency_uq         | 2304 kB | t    | f
 public.verification_items                 | verification_items_leadership_queue_idx         | 2128 kB | f    | f
 public.feed_distribution_completions      | feed_distribution_completions_idempotency_uq    | 1232 kB | t    | f
 public.vaccination_source_facts           | vaccination_source_facts_lineage_unique         | 928 kB  | t    | f
 public.audit_log                          | audit_log_tenant_calendar_event_recorded_idx    | 584 kB  | f    | f
 public.sales_buyer_leads                  | sales_buyer_leads_search_trgm_idx               | 400 kB  | f    | f
 public.feed_wastage_completions           | feed_wastage_completions_idempotency_uq         | 336 kB  | t    | f
 public.herd_register_goat_projection      | herd_register_goat_projection_display_uidx      | 312 kB  | t    | f
 public.sop_task_scan_captures             | sop_task_scan_captures_idempotency_unique_idx   | 304 kB  | t    | f
 public.goats                              | goats_tenant_breed_display_idx                  | 288 kB  | f    | f
 public.sop_task_scan_attempts             | sop_task_scan_attempts_task_goat_idx            | 248 kB  | f    | f
 public.goats                              | goats_tenant_sex_display_idx                    | 224 kB  | f    | f
 public.verification_items                 | verification_items_awaiting_application_idx     | 192 kB  | f    | f
 public.goat_identity_events               | goat_identity_events_tenant_recorded_at_idx     | 184 kB  | f    | f
 public.herd_signal_tag_latest             | herd_signal_tag_latest_movement_idx             | 160 kB  | f    | f
 public.feed_transport_attempts            | feed_transport_attempts_idempotency_uq          | 136 kB  | t    | f
 public.counts_shifting_readiness_evidence | counts_shifting_readiness_evidence_prefix_idx   | 128 kB  | f    | f
 public.procurement_load_goats             | procurement_load_goats_action_idx               | 120 kB  | f    | f
 public.weighing_campaign_sheds            | weighing_campaign_sheds_open_date_idx           | 112 kB  | f    | f
 public.procurement_load_goats             | procurement_load_goats_animal_identifier_1_idx  | 72 kB   | f    | f
 public.feed_transport_attempts            | feed_transport_attempts_number_uq               | 72 kB   | t    | f
 public.count_projection_exceptions        | count_projection_exceptions_queue_idx           | 72 kB   | f    | f
 public.count_projection_exceptions        | count_projection_exceptions_location_list_idx   | 72 kB   | f    | f
 public.locations                          | locations_tenant_status_order_idx               | 64 kB   | f    | f
 public.feed_direction_issues              | feed_direction_issues_idempotency_uidx          | 64 kB   | t    | f
 public.counts_shifting_readiness_evidence | counts_shifting_readiness_evidence_pkey         | 56 kB   | t    | t
 public.sales_fpo_leads                    | sales_fpo_leads_search_trgm_idx                 | 48 kB   | f    | f
 public.count_projection_recompute_runs    | count_projection_recompute_runs_scope_idx       | 48 kB   | f    | f
 public.count_projection_exceptions        | count_projection_exceptions_work_queue_idx      | 48 kB   | f    | f
 public.animal_purchase_candidates         | animal_purchase_candidates_idem_uq              | 40 kB   | t    | f
 public.count_projection_snapshots         | count_projection_snapshots_hot_idx              | 40 kB   | f    | f
 public.identity_decision_goats            | identity_decision_goats_pkey                    | 40 kB   | t    | t
 public.ceo_ai_assistant_audit             | ceo_ai_assistant_audit_conversation_idx         | 40 kB   | f    | f
 public.procurement_vendor_catalog         | procurement_vendor_catalog_side_idx             | 32 kB   | f    | f
 public.workflow_actions                   | workflow_actions_idempotency_uq                 | 32 kB   | t    | f
 public.leadership_tasks                   | leadership_tasks_title_trgm_idx                 | 24 kB   | f    | f
 public.leadership_tasks                   | leadership_tasks_body_trgm_idx                  | 24 kB   | f    | f
 public.vaccination_anchor_events          | vaccination_anchor_events_scope_payload_gin_idx | 24 kB   | f    | f
 public.feed_ration_groups                 | feed_ration_groups_pkey                         | 16 kB   | t    | t
 public.health_config_write_log            | health_config_write_log_pkey                    | 16 kB   | t    | t
 public.feed_session_templates             | feed_session_templates_pkey                     | 16 kB   | t    | t
 public.workforce_positions                | workforce_positions_backup_group_idx            | 16 kB   | f    | f
 public.workforce_members                  | workforce_members_department_idx                | 16 kB   | f    | f
 public.health_protocol_versions           | health_protocol_versions_draft_catalog_idx      | 16 kB   | f    | f
 public.feed_session_templates             | feed_session_templates_natural_key_uidx         | 16 kB   | t    | f
 public.status_definitions                 | status_definitions_axis_active_idx              | 16 kB   | f    | f
 public.sop_task_submission_fanouts        | sop_task_submission_fanouts_pkey                | 16 kB   | t    | t
 public.org_verticals                      | org_verticals_sort_order_unique                 | 16 kB   | t    | f
 public.vaccination_generation_runs        | vaccination_generation_runs_status_idx          | 16 kB   | f    | f
 public.bulk_status_job_row                | bulk_status_job_row_claim_idx                   | 16 kB   | f    | f
 public.sop_versions                       | sop_versions_one_published_per_sop_idx          | 16 kB   | t    | f
 public.identity_decision_goats            | identity_decision_goats_goat_idx                | 16 kB   | f    | f
 public.sop_versions                       | sop_versions_tenant_id_unique                   | 16 kB   | t    | f
 public.workforce_members                  | workforce_members_location_status_idx           | 16 kB   | f    | f
 public.location_operational_attributes    | location_operational_attributes_counts_idx      | 16 kB   | f    | f
 public.org_tiers                          | org_tiers_pkey                                  | 16 kB   | t    | t
 public.goat_location_history              | goat_location_history_tenant_from_location_idx  | 16 kB   | f    | f

 n_unused | total_unused | n_unused_nonunique | nonunique_size 
----------+--------------+--------------------+----------------
      241 | 148 MB       |                100 | 140 MB

## 3b DUPLICATE / PREFIX-OVERLAP INDEXES
                  tbl                   |                       idx_short                        |                          idx_covering                           |  short_sz  |   kind    
----------------------------------------+--------------------------------------------------------+-----------------------------------------------------------------+------------+-----------
 herd_signal_activity_windows           | herd_signal_activity_windows_tag_idx                   | herd_signal_activity_windows_pkey                               | 58 MB      | PREFIX
 notification_delivery_attempts         | notification_delivery_attempts_request_idx             | notification_delivery_attempts_unique                           | 18 MB      | EXACT DUP
 goat_identity_events                   | goat_identity_events_tenant_recorded_at_idx            | goat_identity_events_tenant_recorded_event_idx                  | 184 kB     | PREFIX
 feed_transport_attempts                | feed_transport_attempts_task_history_idx               | feed_transport_attempts_number_uq                               | 72 kB      | EXACT DUP
 bulk_status_job_row                    | bulk_status_job_row_job_idx                            | bulk_status_job_row_unique                                      | 16 kB      | PREFIX
 locations                              | locations_tenant_type_idx                              | locations_tenant_type_status_order_idx                          | 16 kB      | PREFIX
 health_diagnosis_register_versions     | health_diagnosis_register_history_idx                  | health_diagnosis_register_versions_identity_uq                  | 16 kB      | EXACT DUP
 analytics.engagement_daily             | analytics.engagement_daily_tenant_event_date_idx       | analytics.engagement_daily_pkey                                 | 16 kB      | EXACT DUP
 analytics.funnel_daily                 | analytics.funnel_daily_tenant_event_date_idx           | analytics.funnel_daily_pkey                                     | 16 kB      | PREFIX
 analytics.journey_daily                | analytics.journey_daily_tenant_event_date_idx          | analytics.journey_daily_pkey                                    | 16 kB      | PREFIX
 pc_care_task_proofs                    | pc_care_task_proofs_task_idx                           | pc_care_task_proofs_pkey                                        | 16 kB      | PREFIX
 pc_care_task_inventory_requirements    | pc_care_task_inventory_requirements_task_idx           | pc_care_task_inventory_requirements_pkey                        | 16 kB      | PREFIX
 person_module_access                   | person_module_access_person_idx                        | person_module_access_pkey                                       | 16 kB      | PREFIX
 sales_deal_lines                       | sales_deal_lines_deal_idx                              | sales_deal_lines_deal_line_no_uq                                | 16 kB      | EXACT DUP
 animal_purchase_candidates             | animal_purchase_candidates_load_idx                    | animal_purchase_candidates_seq_uq                               | 16 kB      | EXACT DUP
 leadership_tasks                       | leadership_tasks_tenant_deadline_desc_idx              | leadership_tasks_tenant_deadline_idx                            | 16 kB      | EXACT DUP
 health_protocol_versions               | health_protocol_versions_history_idx                   | health_protocol_versions_tenant_id_disease_key_age_band_ver_key | 16 kB      | EXACT DUP
 feed_schedule_config                   | feed_schedule_config_asof_lookup_idx                   | feed_schedule_config_natural_key_uidx                           | 16 kB      | EXACT DUP
 vaccination_operator_assignment_config | vaccination_operator_assignment_config_tenant_park_idx | vaccination_operator_assignment_config_pkey                     | 16 kB      | EXACT DUP
 milk_feeding_attempts                  | milk_feeding_attempts_task_idx                         | milk_feeding_attempts_attempt_uq                                | 16 kB      | EXACT DUP
 milk_preparation_proof_attempts        | milk_preparation_proof_attempts_completion_idx         | milk_preparation_proof_attempts_attempt_uq                      | 16 kB      | EXACT DUP
 procurement_load_prior_outcomes        | procurement_load_prior_outcomes_load_idx               | procurement_load_prior_outcomes_natural_uq                      | 16 kB      | PREFIX
 weighing_fasting_shed_proofs           | weighing_fasting_shed_proofs_task_idx                  | weighing_fasting_shed_proofs_shed_uq                            | 16 kB      | PREFIX
 pc_care_removal_pen_proofs             | pc_care_removal_pen_proofs_task_idx                    | pc_care_removal_pen_proofs_pen_uq                               | 16 kB      | PREFIX
 location_aliases                       | location_aliases_canonical_status_idx                  | location_aliases_canonical_order_idx                            | 8192 bytes | PREFIX
 health_session_steps                   | health_session_steps_order_idx                         | health_session_steps_health_session_id_seq_key                  | 8192 bytes | EXACT DUP
 analytics.crash_daily                  | analytics.crash_daily_tenant_event_date_idx            | analytics.crash_daily_pkey                                      | 8192 bytes | PREFIX
 milk_feeding_watchlist                 | milk_feeding_watchlist_shed_idx                        | milk_feeding_watchlist_pkey                                     | 8192 bytes | EXACT DUP
 feed_direction_completions             | feed_direction_completions_obligation_idx              | feed_direction_completions_obligation_unique                    | 8192 bytes | EXACT DUP
 identity_correction_requests           | identity_correction_requests_queue_idx                 | identity_correction_requests_admin_state_keyset_idx             | 8192 bytes | PREFIX

## 4 FK WITHOUT LEADING INDEX
                     tbl                     |                             conname                             |                  cols                  |                  ref                   |  rows  
---------------------------------------------+-----------------------------------------------------------------+----------------------------------------+----------------------------------------+--------
 audit_log                                   | audit_log_decision_tenant_fk                                    | tenant_id,decision_id                  | identity_decisions                     | 440634
 audit_log                                   | audit_log_decision_id_fkey                                      | decision_id                            | identity_decisions                     | 440634
 notification_delivery_attempts              | notification_delivery_attempts_notification_request_id_fkey     | notification_request_id                | notification_requests                  | 238737
 verification_review_events                  | verification_review_events_proof_fkey                           | proof_id                               | proof_artifacts                        | 105681
 obligation_instances                        | obligation_instances_trigger_tenant_fk                          | tenant_id,generated_by_trigger_id      | protocol_triggers                      |  86616
 feed_direction_issue_rows                   | feed_direction_issue_rows_feed_direction_issue_id_fkey          | feed_direction_issue_id                | feed_direction_issues                  |  86182
 idempotency_keys                            | idempotency_keys_tenant_id_fkey                                 | tenant_id                              | tenants                                |  47682
 feed_packing_verified_quantities            | feed_packing_verified_quantities_completion_fk                  | completion_id                          | feed_packing_completions               |   8834
 feed_distribution_completions               | feed_distribution_completions_shed_fk                           | tenant_id,shed_id                      | locations                              |   8375
 feed_packing_completions                    | feed_packing_completions_shed_fk                                | tenant_id,shed_id                      | locations                              |   8040
 vaccination_completions                     | vaccination_completions_lot_tenant_fk                           | tenant_id,vaccine_inventory_lot_id     | inventory_stock                        |   6098
 weighing_observations                       | weighing_observations_proof_artifact_id_fkey                    | proof_artifact_id                      | proof_artifacts                        |   2852
 weighing_observations                       | weighing_observations_campaign_shed_id_fkey                     | campaign_shed_id                       | weighing_campaign_sheds                |   2852
 weighing_observations                       | weighing_observations_campaign_id_fkey                          | campaign_id                            | weighing_campaigns                     |   2852
 goat_identity_events                        | goat_identity_events_decision_id_fkey                           | decision_id                            | identity_decisions                     |   2318
 goat_identity_events                        | goat_identity_events_decision_tenant_fk                         | tenant_id,decision_id                  | identity_decisions                     |   2318
 pc_care_task_animals                        | pc_care_task_animals_task_id_fkey                               | task_id                                | pc_care_tasks                          |   2181
 feed_transport_tasks                        | feed_transport_tasks_park_fk                                    | tenant_id,park_id                      | locations                              |   2177
 feed_transport_tasks                        | feed_transport_tasks_current_attempt_fk                         | tenant_id,current_attempt_id           | feed_transport_attempts                |   2177
 feed_transport_tasks                        | feed_transport_tasks_shed_fk                                    | tenant_id,shed_id                      | locations                              |   2177
 goats                                       | goats_farm_tenant_fk                                            | tenant_id,farm_id                      | locations                              |   1741
 goats                                       | goats_merged_into_tenant_fk                                     | tenant_id,merged_into_goat_id          | goats                                  |   1741
 goats                                       | goats_park_tenant_fk                                            | tenant_id,park_id                      | locations                              |   1741
 goats                                       | goats_shed_tenant_fk                                            | tenant_id,shed_id                      | locations                              |   1741
 goats                                       | goats_cohort_tenant_fk                                          | tenant_id,cohort_id                    | locations                              |   1741
 goats                                       | goats_current_location_tenant_fk                                | tenant_id,current_location_id          | locations                              |   1741
 goats                                       | goats_custodian_party_id_fkey                                   | custodian_party_id                     | parties                                |   1741
 sop_task_scan_attempts                      | sop_task_scan_attempts_goat_id_fkey                             | goat_id                                | goats                                  |   1574
 sop_task_scan_attempts                      | sop_task_scan_attempts_task_id_fkey                             | task_id                                | sop_tasks                              |   1574
 sop_submission_items                        | sop_submission_items_submission_id_fkey                         | submission_id                          | sop_submissions                        |   1427
 sop_submission_items                        | sop_submission_items_task_id_fkey                               | task_id                                | sop_tasks                              |   1427
 sop_submission_items                        | sop_submission_items_goat_id_fkey                               | goat_id                                | goats                                  |   1427
 feed_wastage_completions                    | feed_wastage_completions_shed_fk                                | tenant_id,shed_id                      | locations                              |   1134
 sop_task_scan_captures                      | sop_task_scan_captures_task_id_fkey                             | task_id                                | sop_tasks                              |   1114
 sop_task_scan_captures                      | sop_task_scan_captures_goat_id_fkey                             | goat_id                                | goats                                  |   1114
 health_protocol_steps                       | health_protocol_steps_medicine_item_fk                          | tenant_id,medicine_item_id             | inventory_items                        |   1068
 health_protocol_steps                       | health_protocol_steps_protocol_fk                               | tenant_id,health_protocol_version_id   | health_protocol_versions               |   1068
 obligation_batches                          | obligation_batches_lot_tenant_fk                                | tenant_id,primary_inventory_lot_id     | inventory_stock                        |    776
 person_module_access                        | person_module_access_workforce_member_id_fkey                   | workforce_member_id                    | workforce_members                      |    747
 vaccination_drive_assignments               | vaccination_drive_assignments_tenant_shed_fk                    | tenant_id,shed_id                      | locations                              |    740
 vaccination_drive_assignments               | vaccination_drive_assignments_operator_fk                       | operator_id                            | workforce_members                      |    740
 procurement_vendors                         | procurement_vendors_party_fk                                    | party_id                               | parties                                |    671
 protocol_rule_dimensions                    | protocol_rule_dimensions_protocol_version_id_fkey               | protocol_version_id                    | protocol_versions                      |    528
 protocol_rule_dimensions                    | protocol_rule_dimensions_rule_id_fkey                           | rule_id                                | protocol_rules                         |    528
 ceo_ai_messages                             | ceo_ai_messages_tenant_id_fkey                                  | tenant_id                              | tenants                                |    520
 sop_tasks                                   | sop_tasks_sop_id_fkey                                           | sop_id                                 | sop_definitions                        |    487
 sop_tasks                                   | sop_tasks_sop_version_id_fkey                                   | sop_version_id                         | sop_versions                           |    487
 identity_decision_events                    | identity_decision_events_event_fk                               | event_id,event_recorded_at             | goat_identity_events                   |    435
 identity_decision_events                    | identity_decision_events_decision_tenant_fk                     | decision_id,tenant_id                  | identity_decisions                     |    435
 identity_decision_events                    | identity_decision_events_tenant_id_fkey                         | tenant_id                              | tenants                                |    435
 identity_decision_events                    | identity_decision_events_event_tenant_fk                        | tenant_id,event_id,event_recorded_at   | goat_identity_events                   |    435
 procurement_load_goats                      | procurement_load_goats_holding_location_tenant_fk               | tenant_id,holding_location_id          | locations                              |    406
 workforce_member_devices                    | workforce_member_devices_workforce_member_id_fkey               | workforce_member_id                    | workforce_members                      |    397
 milk_feeding_tasks                          | milk_feeding_tasks_shed_fk                                      | tenant_id,shed_id                      | locations                              |    392
 workforce_clock_events                      | workforce_clock_events_workforce_member_id_fkey                 | workforce_member_id                    | workforce_members                      |    390
 weighing_campaign_sheds                     | weighing_campaign_sheds_campaign_id_fkey                        | campaign_id                            | weighing_campaigns                     |    377
 weighing_campaign_sheds                     | weighing_campaign_sheds_location_id_fkey                        | location_id                            | locations                              |    377
 weighing_work_items                         | weighing_work_items_shed_location_id_fkey                       | shed_location_id                       | locations                              |    311
 weighing_work_items                         | weighing_work_items_campaign_shed_id_fkey                       | campaign_shed_id                       | weighing_campaign_sheds                |    311
 weighing_work_items                         | weighing_work_items_merged_into_work_item_id_fkey               | merged_into_work_item_id               | weighing_work_items                    |    311
 weighing_work_items                         | weighing_work_items_park_id_fkey                                | park_id                                | locations                              |    311
 weighing_work_items                         | weighing_work_items_campaign_id_fkey                            | campaign_id                            | weighing_campaigns                     |    311
 animal_purchase_candidate_media             | animal_purchase_candidate_media_candidate_fk                    | candidate_id                           | animal_purchase_candidates             |    309
 pc_care_tasks                               | pc_care_tasks_shed_fk                                           | tenant_id,shed_id                      | locations                              |    284
 pc_care_tasks                               | pc_care_tasks_gates_round_fk                                    | gates_round_id                         | pc_care_rounds                         |    284
 pc_care_tasks                               | pc_care_tasks_round_fk                                          | round_id                               | pc_care_rounds                         |    284
 identity_decision_goats                     | identity_decision_goats_decision_tenant_fk                      | decision_id,tenant_id                  | identity_decisions                     |    277
 identity_decision_goats                     | identity_decision_goats_tenant_id_fkey                          | tenant_id                              | tenants                                |    277
 identity_decision_goats                     | identity_decision_goats_goat_tenant_fk                          | tenant_id,goat_id                      | goats                                  |    277
 pc_care_task_assignees                      | pc_care_task_assignees_task_id_fkey                             | task_id                                | pc_care_tasks                          |    254
 workforce_clock_entries                     | workforce_clock_entries_clock_out_event_id_fkey                 | clock_out_event_id                     | workforce_clock_events                 |    252
 workforce_clock_entries                     | workforce_clock_entries_workforce_member_id_fkey                | workforce_member_id                    | workforce_members                      |    252
 workforce_clock_entries                     | workforce_clock_entries_clock_in_event_id_fkey                  | clock_in_event_id                      | workforce_clock_events                 |    252
 feed_experiment_config                      | feed_experiment_config_shed_fk                                  | tenant_id,shed_id                      | locations                              |    210
 protocol_rules                              | protocol_rules_sop_version_tenant_fk                            | tenant_id,sop_version_id               | sop_versions                           |    204
 goat_location_history                       | goat_location_history_goat_tenant_fk                            | tenant_id,goat_id                      | goats                                  |    196
 goat_location_history                       | goat_location_history_from_location_id_fkey                     | from_location_id                       | locations                              |    196
 shed_profiles                               | shed_profiles_lifecycle_status_tenant_fk                        | tenant_id,shed_lifecycle_status_id     | shed_lifecycle_status_lookup           |    175
 shed_profiles                               | shed_profiles_tenant_id_fkey                                    | tenant_id                              | tenants                                |    175
 shed_profiles                               | shed_profiles_animal_stage_tenant_fk                            | tenant_id,animal_stage_id              | animal_stage_lookup                    |    175
 shed_profiles                               | shed_profiles_location_tenant_fk                                | location_id,tenant_id                  | locations                              |    175
 identity_decision_identifiers               | identity_decision_identifiers_identifier_tenant_fk              | tenant_id,identifier_id                | goat_identifiers                       |    174
 identity_decision_identifiers               | identity_decision_identifiers_tenant_id_fkey                    | tenant_id                              | tenants                                |    174
 identity_decision_identifiers               | identity_decision_identifiers_decision_tenant_fk                | decision_id,tenant_id                  | identity_decisions                     |    174
 location_operational_attributes             | location_operational_attributes_tenant_location_fk              | tenant_id,location_id                  | locations                              |    163
 count_projection_recompute_runs             | count_projection_recompute_runs_park_id_fkey                    | park_id                                | locations                              |    138
 count_projection_recompute_runs             | count_projection_recompute_runs_snapshot_id_fkey                | snapshot_id                            | count_projection_snapshots             |    138
 shed_partitions                             | shed_partitions_animal_stage_tenant_fk                          | tenant_id,animal_stage_id              | animal_stage_lookup                    |    130
 pen_visit_tasks                             | pen_visit_tasks_proof_ref_fkey                                  | proof_ref                              | proof_artifacts                        |    129
 count_projection_snapshots                  | count_projection_snapshots_tenant_park_fk                       | tenant_id,park_id                      | locations                              |    110
 count_projection_snapshots                  | count_projection_snapshots_park_id_fkey                         | park_id                                | locations                              |    110
 weighing_fasting_shed_proofs                | weighing_fasting_shed_proofs_fasting_task_id_fkey               | fasting_task_id                        | weighing_fasting_tasks                 |    107
 milk_feeding_attempts                       | milk_feeding_attempts_task_id_fkey                              | task_id                                | milk_feeding_tasks                     |    107
 weighing_fasting_shed_proofs                | weighing_fasting_shed_proofs_campaign_shed_id_fkey              | campaign_shed_id                       | weighing_campaign_sheds                |    107
 goat_sale_allocations                       | goat_sale_allocations_goat_fkey                                 | goat_id                                | goats                                  |    104
 market_price_entries                        | market_price_entries_city_id_fkey                               | city_id                                | market_cities                          |    102
 market_price_entries                        | market_price_entries_question_id_fkey                           | question_id                            | market_questions                       |    102
 weighing_shed_observations                  | weighing_shed_observations_campaign_shed_id_fkey                | campaign_shed_id                       | weighing_campaign_sheds                |     95
 weighing_shed_observations                  | weighing_shed_observations_proof_artifact_id_fkey               | proof_artifact_id                      | proof_artifacts                        |     95
 weighing_shed_observations                  | weighing_shed_observations_campaign_id_fkey                     | campaign_id                            | weighing_campaigns                     |     95
 sop_submissions                             | sop_submissions_task_id_fkey                                    | task_id                                | sop_tasks                              |     80
 sop_submissions                             | sop_submissions_sop_version_id_fkey                             | sop_version_id                         | sop_versions                           |     80
 user_scope_grants                           | user_scope_grants_role_fk                                       | role                                   | org_role_catalog                       |     71
 sales_deal_lines                            | sales_deal_lines_deal_fk                                        | deal_id                                | sales_deals                            |     71
 shifting_events                             | shifting_events_destination_park_id_fkey                        | destination_park_id                    | locations                              |     69
 shifting_events                             | shifting_events_source_shed_id_fkey                             | source_shed_id                         | locations                              |     69
 shifting_events                             | shifting_events_source_park_id_fkey                             | source_park_id                         | locations                              |     69
 shifting_events                             | shifting_events_destination_shed_id_fkey                        | destination_shed_id                    | locations                              |     69
 shifting_events                             | shifting_events_tenant_source_shed_fk                           | tenant_id,source_shed_id               | locations                              |     69
 shifting_events                             | shifting_events_tenant_destination_shed_fk                      | tenant_id,destination_shed_id          | locations                              |     69
 count_projection_exceptions                 | count_projection_exceptions_tenant_snapshot_fk                  | tenant_id,count_projection_snapshot_id | count_projection_snapshots             |     67
 count_projection_exceptions                 | count_projection_exceptions_tenant_shed_fk                      | tenant_id,shed_id                      | locations                              |     67
 count_projection_exceptions                 | count_projection_exceptions_tenant_park_fk                      | tenant_id,park_id                      | locations                              |     67
 count_projection_exceptions                 | count_projection_exceptions_shed_id_fkey                        | shed_id                                | locations                              |     67
 count_projection_exceptions                 | count_projection_exceptions_resolution_id_fkey                  | resolution_id                          | count_projection_exception_resolutions |     67
 count_projection_exceptions                 | count_projection_exceptions_park_id_fkey                        | park_id                                | locations                              |     67
 count_projection_exceptions                 | count_projection_exceptions_count_projection_snapshot_id_fkey   | count_projection_snapshot_id           | count_projection_snapshots             |     67
 animal_purchase_candidates                  | animal_purchase_candidates_load_fk                              | load_id                                | animal_purchase_loads                  |     62
 shifting_event_impacts                      | shifting_event_impacts_shifting_event_id_fkey                   | shifting_event_id                      | shifting_events                        |     52
 shifting_event_impacts                      | shifting_event_impacts_breed_id_fkey                            | breed_id                               | breeds                                 |     52
 pen_visit_task_sources                      | pen_visit_task_sources_task_id_fkey                             | task_id                                | pen_visit_tasks                        |     52
 weighing_shed_observation_proofs            | weighing_shed_observation_proofs_proof_artifact_id_fkey         | proof_artifact_id                      | proof_artifacts                        |     50
 pc_care_removal_pen_proofs                  | pc_care_removal_pen_proofs_gated_fk                             | tenant_id,gated_task_id                | pc_care_tasks                          |     45
 milk_preparation_completions                | milk_preparation_completions_shed_fk                            | tenant_id,shed_id                      | locations                              |     42
 workforce_members                           | workforce_members_primary_location_id_fkey                      | primary_location_id                    | locations                              |     40
 weighing_campaigns                          | weighing_campaigns_tenant_id_fkey                               | tenant_id                              | tenants                                |     40
 weighing_campaigns                          | weighing_campaigns_park_id_fkey                                 | park_id                                | locations                              |     40
 person_access                               | person_access_designation_code_fkey                             | designation_code                       | designation_catalog                    |     31
 person_access                               | person_access_workforce_member_id_fkey                          | workforce_member_id                    | workforce_members                      |     31
 person_park_scope                           | person_park_scope_park_id_fkey                                  | park_id                                | locations                              |     28
 person_park_scope                           | person_park_scope_workforce_member_id_fkey                      | workforce_member_id                    | workforce_members                      |     28
 auth_pending_email_grants                   | auth_pending_email_grants_role_fk                               | role                                   | org_role_catalog                       |     22
 breed_aliases                               | breed_aliases_breed_id_fkey                                     | breed_id                               | breeds                                 |     19
 sop_versions                                | sop_versions_sop_id_fkey                                        | sop_id                                 | sop_definitions                        |     17
 toxin_test_tasks                            | toxin_test_tasks_retest_of_task_id_fkey                         | retest_of_task_id                      | toxin_test_tasks                       |     14
 toxin_test_tasks                            | toxin_test_tasks_superseded_by_task_id_fkey                     | superseded_by_task_id                  | toxin_test_tasks                       |     14
 procurement_loads                           | procurement_loads_source_location_id_fkey                       | source_location_id                     | locations                              |      8
 weighing_fasting_tasks                      | weighing_fasting_tasks_campaign_id_fkey                         | campaign_id                            | weighing_campaigns                     |      8
 procurement_loads                           | procurement_loads_source_party_id_fkey                          | source_party_id                        | parties                                |      8
 procurement_loads                           | procurement_loads_source_location_tenant_fk                     | tenant_id,source_location_id           | locations                              |      8
 weighing_fasting_tasks                      | weighing_fasting_tasks_park_id_fkey                             | park_id                                | locations                              |      8
 workforce_positions                         | workforce_positions_workforce_member_id_fkey                    | workforce_member_id                    | workforce_members                      |      3
 pen_visit_park_assignees                    | pen_visit_park_assignees_park_id_fkey                           | park_id                                | locations                              |      2
 protocol_versions                           | protocol_versions_sop_version_tenant_fk                         | tenant_id,sop_version_id               | sop_versions                           |      2
 workforce_member_browser_push_registrations | workforce_member_browser_push_registrations_member_id_fkey      | workforce_member_id                    | workforce_members                      |      1
 admin_ui_config_family_change_queue         | admin_ui_config_family_change_queue_tenant_id_fkey              | tenant_id                              | tenants                                |      0
 sop_task_review_fanouts                     | sop_task_review_fanouts_task_id_fkey                            | task_id                                | sop_tasks                              |     -1
 sop_task_submission_fanouts                 | sop_task_submission_fanouts_submission_id_fkey                  | submission_id                          | sop_submissions                        |     -1
 sop_task_submission_fanouts                 | sop_task_submission_fanouts_task_id_fkey                        | task_id                                | sop_tasks                              |     -1
 identity_decision_media                     | identity_decision_media_decision_tenant_fk                      | decision_id,tenant_id                  | identity_decisions                     |     -1
 identity_correction_requests                | identity_correction_requests_shed_tenant_fk                     | tenant_id,shed_id                      | locations                              |     -1
 source_entry_decisions                      | source_entry_decisions_owner_fk                                 | owner_id                               | workforce_members                      |     -1
 source_entry_decisions                      | source_entry_decisions_proof_ref_id_fkey                        | proof_ref_id                           | proof_artifacts                        |     -1
 source_entry_decisions                      | source_entry_decisions_task_tenant_fk                           | tenant_id,sop_task_id                  | sop_tasks                              |     -1
 source_holding_stays                        | source_holding_stays_location_tenant_fk                         | tenant_id,holding_location_id          | locations                              |     -1
 identity_correction_requests                | identity_correction_requests_shed_id_fkey                       | shed_id                                | locations                              |     -1
 transit_handoffs                            | transit_handoffs_from_location_tenant_fk                        | tenant_id,from_location_id             | locations                              |     -1
 transit_handoffs                            | transit_handoffs_proof_ref_id_fkey                              | proof_ref_id                           | proof_artifacts                        |     -1
 transit_handoffs                            | transit_handoffs_to_location_tenant_fk                          | tenant_id,to_location_id               | locations                              |     -1
 identity_correction_requests                | identity_correction_requests_park_tenant_fk                     | tenant_id,park_id                      | locations                              |     -1
 identity_correction_requests                | identity_correction_requests_park_id_fkey                       | park_id                                | locations                              |     -1
 vaccination_generation_runs                 | vaccination_generation_runs_cursor_goat_fk                      | tenant_id,cursor_goat_id               | goats                                  |     -1
 workforce_absences                          | workforce_absences_replacement_member_id_fkey                   | replacement_member_id                  | workforce_members                      |     -1
 workforce_absences                          | workforce_absences_workforce_member_id_fkey                     | workforce_member_id                    | workforce_members                      |     -1
 workforce_external_identities               | workforce_external_identities_workforce_member_id_fkey          | workforce_member_id                    | workforce_members                      |     -1
 workforce_member_app_sessions               | workforce_member_app_sessions_device_id_fkey                    | device_id                              | workforce_member_devices               |     -1
 workforce_member_app_sessions               | workforce_member_app_sessions_workforce_member_id_fkey          | workforce_member_id                    | workforce_members                      |     -1
 workforce_member_capabilities               | workforce_member_capabilities_capability_id_fkey                | capability_id                          | workforce_capabilities                 |     -1
 workforce_member_capabilities               | workforce_member_capabilities_workforce_member_id_fkey          | workforce_member_id                    | workforce_members                      |     -1
 identity_correction_requests                | identity_correction_requests_goat_tenant_fk                     | tenant_id,goat_id                      | goats                                  |     -1
 workforce_roster_assignments                | workforce_roster_assignments_workforce_member_id_fkey           | workforce_member_id                    | workforce_members                      |     -1
 feed_shed_factors                           | feed_shed_factors_shed_fk                                       | tenant_id,shed_id                      | locations                              |     -1
 feed_purchase_payments                      | feed_purchase_payments_feed_purchase_id_fkey                    | feed_purchase_id                       | feed_purchases                         |     -1
 pen_routine_versions                        | pen_routine_versions_routine_id_fkey                            | routine_id                             | pen_routine_definitions                |     -1
 identity_correction_requests                | identity_correction_requests_farm_tenant_fk                     | tenant_id,farm_id                      | locations                              |     -1
 workforce_member_titles                     | workforce_member_titles_workforce_member_id_fkey                | workforce_member_id                    | workforce_members                      |     -1
 identity_correction_requests                | identity_correction_requests_farm_id_fkey                       | farm_id                                | locations                              |     -1
 identity_correction_requests                | identity_correction_requests_decision_tenant_fk                 | tenant_id,decision_id                  | identity_decisions                     |     -1
 identity_correction_requests                | identity_correction_requests_decision_id_fkey                   | decision_id                            | identity_decisions                     |     -1
 identity_correction_requests                | identity_correction_requests_cohort_tenant_fk                   | tenant_id,cohort_id                    | locations                              |     -1
 vaccination_operator_assignment_config      | vaccination_operator_assignment_config_park_fk                  | park_id                                | locations                              |     -1
 vaccination_operator_assignment_config      | vaccination_operator_assignment_config_default_operator_fk      | default_operator_id                    | workforce_members                      |     -1
 vaccination_operator_shift_config           | vaccination_operator_shift_config_park_fk                       | park_id                                | locations                              |     -1
 vaccination_operator_shift_config           | vaccination_operator_shift_config_operator_fk                   | operator_id                            | workforce_members                      |     -1
 vaccination_operator_capacity_overrides     | vaccination_operator_capacity_overrides_park_fk                 | park_id                                | locations                              |     -1
 vaccination_operator_capacity_overrides     | vaccination_operator_capacity_overrides_operator_fk             | operator_id                            | workforce_members                      |     -1
 configuration_import_jobs                   | configuration_import_jobs_bundle_id_fkey                        | bundle_id                              | configuration_import_bundles           |     -1
 arrival_intake_review_goats                 | arrival_intake_review_goats_goat_tenant_fk                      | tenant_id,goat_id                      | goats                                  |     -1
 identity_correction_requests                | identity_correction_requests_cohort_id_fkey                     | cohort_id                              | locations                              |     -1
 identity_conflicts                          | identity_conflicts_decision_tenant_fk                           | tenant_id,decision_id                  | identity_decisions                     |     -1
 pc_care_rounds                              | pc_care_rounds_park_id_fkey                                     | park_id                                | locations                              |     -1
 identity_conflict_source_records            | identity_conflict_source_records_tenant_id_fkey                 | tenant_id                              | tenants                                |     -1
 identity_conflict_source_records            | identity_conflict_source_records_conflict_tenant_fk             | conflict_id,tenant_id                  | identity_conflicts                     |     -1
 pen_routine_pens                            | pen_routine_pens_routine_id_fkey                                | routine_id                             | pen_routine_definitions                |     -1
 identity_conflict_goats                     | identity_conflict_goats_tenant_id_fkey                          | tenant_id                              | tenants                                |     -1
 identity_conflict_goats                     | identity_conflict_goats_goat_tenant_fk                          | tenant_id,goat_id                      | goats                                  |     -1
 identity_conflict_goats                     | identity_conflict_goats_conflict_tenant_fk                      | conflict_id,tenant_id                  | identity_conflicts                     |     -1
 goat_ownership                              | goat_ownership_tenant_id_fkey                                   | tenant_id                              | tenants                                |     -1
 goat_ownership                              | goat_ownership_goat_tenant_fk                                   | tenant_id,goat_id                      | goats                                  |     -1
 goat_ownership                              | goat_ownership_decision_tenant_fk                               | tenant_id,decision_id                  | identity_decisions                     |     -1
 goat_ownership                              | goat_ownership_decision_id_fkey                                 | decision_id                            | identity_decisions                     |     -1
 feed_direction_session_completions          | feed_direction_session_completions_shed_fk                      | tenant_id,shed_id                      | locations                              |     -1
 goat_merge_links                            | goat_merge_links_tenant_id_fkey                                 | tenant_id                              | tenants                                |     -1
 goat_merge_links                            | goat_merge_links_survivor_tenant_fk                             | tenant_id,survivor_goat_id             | goats                                  |     -1
 goat_merge_links                            | goat_merge_links_merged_tenant_fk                               | tenant_id,merged_goat_id               | goats                                  |     -1
 goat_merge_links                            | goat_merge_links_decision_tenant_fk                             | tenant_id,decision_id                  | identity_decisions                     |     -1
 goat_merge_links                            | goat_merge_links_decision_id_fkey                               | decision_id                            | identity_decisions                     |     -1
 goat_custody_history                        | goat_custody_history_to_location_tenant_fk                      | tenant_id,to_location_id               | locations                              |     -1
 goat_custody_history                        | goat_custody_history_to_location_id_fkey                        | to_location_id                         | locations                              |     -1
 goat_custody_history                        | goat_custody_history_tenant_id_fkey                             | tenant_id                              | tenants                                |     -1
 goat_custody_history                        | goat_custody_history_goat_tenant_fk                             | tenant_id,goat_id                      | goats                                  |     -1
 goat_custody_history                        | goat_custody_history_from_location_tenant_fk                    | tenant_id,from_location_id             | locations                              |     -1
 goat_custody_history                        | goat_custody_history_from_location_id_fkey                      | from_location_id                       | locations                              |     -1
 goat_custody_history                        | goat_custody_history_decision_tenant_fk                         | tenant_id,decision_id                  | identity_decisions                     |     -1
 goat_custody_history                        | goat_custody_history_decision_id_fkey                           | decision_id                            | identity_decisions                     |     -1
 feed_direction_completions                  | feed_direction_completions_submission_item_tenant_fk            | tenant_id,sop_submission_item_id       | sop_submission_items                   |     -1
 milk_feeding_watchlist                      | milk_feeding_watchlist_goat_fk                                  | tenant_id,goat_id                      | goats                                  |     -1
 milk_feeding_farm_watchlist                 | milk_feeding_farm_watchlist_goat_fk                             | tenant_id,goat_id                      | goats                                  |     -1
 feed_direction_completions                  | feed_direction_completions_lot_tenant_fk                        | tenant_id,feed_inventory_lot_id        | inventory_stock                        |     -1
 health_cases                                | health_cases_protocol_fk                                        | tenant_id,health_protocol_version_id   | health_protocol_versions               |     -1
 health_treatment_sessions                   | health_sessions_case_fk                                         | tenant_id,health_case_id               | health_cases                           |     -1
 health_session_steps                        | health_session_steps_session_fk                                 | tenant_id,health_session_id            | health_treatment_sessions              |     -1
 health_medicine_administrations             | health_administrations_case_fk                                  | tenant_id,health_case_id               | health_cases                           |     -1
 health_medicine_administrations             | health_administrations_session_fk                               | tenant_id,health_session_id            | health_treatment_sessions              |     -1
 farm_profiles                               | farm_profiles_tenant_id_fkey                                    | tenant_id                              | tenants                                |     -1
 procurement_load_prior_outcomes             | procurement_load_prior_outcomes_load_fkey                       | load_id                                | procurement_loads                      |     -1
 farm_profiles                               | farm_profiles_location_tenant_fk                                | location_id,tenant_id                  | locations                              |     -1
 leadership_task_attachments                 | leadership_task_attachments_task_id_fkey                        | task_id                                | leadership_tasks                       |     -1
 leadership_task_attachments                 | leadership_task_attachments_proof_id_fkey                       | proof_id                               | proof_artifacts                        |     -1
 count_projection_snapshot_rows              | count_projection_snapshot_rows_tenant_shed_fk                   | tenant_id,shed_id                      | locations                              |     -1
 count_projection_snapshot_rows              | count_projection_snapshot_rows_tenant_park_fk                   | tenant_id,park_id                      | locations                              |     -1
 count_projection_snapshot_rows              | count_projection_snapshot_rows_shed_id_fkey                     | shed_id                                | locations                              |     -1
 count_projection_snapshot_rows              | count_projection_snapshot_rows_park_id_fkey                     | park_id                                | locations                              |     -1
 count_projection_snapshot_rows              | count_projection_snapshot_rows_breed_id_fkey                    | breed_id                               | breeds                                 |     -1
 count_projection_snapshot_rows              | count_projection_snapshot_row_count_projection_snapshot_id_fkey | count_projection_snapshot_id           | count_projection_snapshots             |     -1
 workforce_leave_requests                    | workforce_leave_requests_workforce_member_id_fkey               | workforce_member_id                    | workforce_members                      |     -1
 count_projection_exception_resolutions      | count_projection_exception_re_count_projection_exception_i_fkey | count_projection_exception_id          | count_projection_exceptions            |     -1
 count_mismatch_scan_runs                    | count_mismatch_scan_runs_tenant_shed_fk                         | tenant_id,shed_id                      | locations                              |     -1
 count_mismatch_scan_runs                    | count_mismatch_scan_runs_shed_id_fkey                           | shed_id                                | locations                              |     -1
 count_mismatch_scan_runs                    | count_mismatch_scan_runs_park_id_fkey                           | park_id                                | locations                              |     -1
 count_base_anchors                          | count_base_anchors_tenant_shed_fk                               | tenant_id,shed_id                      | locations                              |     -1
 count_base_anchors                          | count_base_anchors_shed_id_fkey                                 | shed_id                                | locations                              |     -1
 count_base_anchors                          | count_base_anchors_park_id_fkey                                 | park_id                                | locations                              |     -1
 count_base_anchors                          | count_base_anchors_breed_id_fkey                                | breed_id                               | breeds                                 |     -1
 leadership_task_notes                       | leadership_task_notes_task_id_fkey                              | task_id                                | leadership_tasks                       |     -1
 toxin_test_step_completions                 | toxin_test_step_completions_task_id_fkey                        | task_id                                | toxin_test_tasks                       |     -1
 configuration_import_rows                   | configuration_import_rows_job_id_fkey                           | job_id                                 | configuration_import_jobs              |     -1
 leadership_task_mentions                    | leadership_task_mentions_task_id_fkey                           | task_id                                | leadership_tasks                       |     -1
 leadership_task_mentions                    | leadership_task_mentions_note_id_fkey                           | note_id                                | leadership_task_notes                  |     -1
 leadership_task_events                      | leadership_task_events_task_id_fkey                             | task_id                                | leadership_tasks                       |     -1
 leadership_task_events                      | leadership_task_events_note_id_fkey                             | note_id                                | leadership_task_notes                  |     -1
 calendar_snoozes                            | calendar_snoozes_replaced_fk                                    | replaced_by_snooze_id                  | calendar_snoozes                       |     -1
 arrival_intake_reviews                      | arrival_intake_reviews_park_tenant_fk                           | tenant_id,park_location_id             | locations                              |     -1
 arrival_intake_reviews                      | arrival_intake_reviews_media_proof_id_fkey                      | media_proof_id                         | proof_artifacts                        |     -1
 arrival_intake_review_goats                 | arrival_intake_review_goats_proof_ref_id_fkey                   | proof_ref_id                           | proof_artifacts                        |     -1
 pen_routine_definitions                     | pen_routine_definitions_park_id_fkey                            | park_id                                | locations                              |     -1
 pen_routine_tasks                           | pen_routine_tasks_routine_id_fkey                               | routine_id                             | pen_routine_definitions                |     -1
 pen_routine_task_presence                   | pen_routine_task_presence_task_id_fkey                          | task_id                                | pen_routine_tasks                      |     -1
 item_categories                             | item_categories_parent_category_id_fkey                         | parent_category_id                     | item_categories                        |     -1
 leadership_task_participants                | leadership_task_participants_task_id_fkey                       | task_id                                | leadership_tasks                       |     -1
 procurement_source_health_checks            | procurement_source_health_checks_task_tenant_fk                 | tenant_id,sop_task_id                  | sop_tasks                              |     -1
 procurement_source_health_checks            | procurement_source_health_checks_load_tenant_fk                 | tenant_id,load_id                      | procurement_loads                      |     -1
 procurement_source_health_checks            | procurement_source_health_checks_proof_ref_id_fkey              | proof_ref_id                           | proof_artifacts                        |     -1
 procurement_pc_handoffs                     | procurement_pc_handoffs_shed_tenant_fk                          | tenant_id,shed_location_id             | locations                              |     -1
 procurement_pc_handoffs                     | procurement_pc_handoffs_park_tenant_fk                          | tenant_id,park_location_id             | locations                              |     -1
 procurement_hf_vaccination_evidence         | procurement_hf_vaccination_evidence_rule_tenant_fk              | tenant_id,rule_id                      | protocol_rules                         |     -1
 procurement_hf_vaccination_evidence         | procurement_hf_vaccination_evidence_protocol_version_tenant_fk  | tenant_id,protocol_version_id          | protocol_versions                      |     -1
 procurement_hf_vaccination_evidence         | procurement_hf_vaccination_evidence_proof_fk                    | proof_ref_id                           | proof_artifacts                        |     -1
 park_profiles                               | park_profiles_tenant_id_fkey                                    | tenant_id                              | tenants                                |     -1
 park_profiles                               | park_profiles_location_tenant_fk                                | location_id,tenant_id                  | locations                              |     -1
 obligation_goat_shift_watermarks            | obligation_goat_shift_watermarks_last_scope_id_fkey             | last_scope_id                          | locations                              |     -1
 obligation_goat_shift_watermarks            | obligation_goat_shift_watermarks_goat_id_fkey                   | goat_id                                | goats                                  |     -1
 movement_commands                           | movement_commands_task_id_fkey                                  | task_id                                | sop_tasks                              |     -1
 movement_commands                           | movement_commands_submission_id_fkey                            | submission_id                          | sop_submissions                        |     -1
 location_review_items                       | location_review_items_canonical_location_id_fkey                | canonical_location_id                  | locations                              |     -1
 location_projection_invalidations           | location_projection_invalidations_affected_location_id_fkey     | affected_location_id                   | locations                              |     -1
 location_capacity_records                   | location_capacity_records_location_id_fkey                      | location_id                            | locations                              |     -1
 location_aliases                            | location_aliases_canonical_location_id_fkey                     | canonical_location_id                  | locations                              |     -1
 inventory_stock_movements                   | inventory_stock_movements_lot_tenant_fk                         | tenant_id,lot_id,item_id,location_id   | inventory_stock                        |     -1
 inventory_stock                             | inventory_stock_item_tenant_fk                                  | tenant_id,item_id                      | inventory_items                        |     -1
 identity_decision_media                     | identity_decision_media_tenant_id_fkey                          | tenant_id                              | tenants                                |     -1

## 5 BLOAT dead ratio
                 tbl                  | n_live_tup | n_dead_tup | dead_pct | n_mod_since_analyze |        last_autovacuum        | last_vacuum | autovacuum_count 
--------------------------------------+------------+------------+----------+---------------------+-------------------------------+-------------+------------------
 public.herd_signal_activity_windows  |     749170 |      65646 |      8.1 |                5817 | 2026-09-21 05:00:58.491174+00 |             |               30
 public.outbox_messages               |     362562 |      57667 |     13.7 |               10421 | 2026-09-19 16:40:02.89827+00  |             |               57
 public.notification_requests         |     238710 |      30754 |     11.4 |               18737 | 2026-09-21 05:54:05.347105+00 |             |               74
 public.domain_event_processed_events |      87761 |      14370 |     14.1 |                7062 | 2026-09-23 07:01:31.229856+00 |             |               81
 public.obligation_instances          |      86684 |       5636 |      6.1 |                 813 | 2026-09-05 06:07:07.529031+00 |             |               84
 public.verification_items            |      22564 |       3960 |     14.9 |                 469 | 2026-09-16 11:35:17.246383+00 |             |               10
 public.proof_artifacts               |      45821 |       1619 |      3.4 |                1462 | 2026-09-22 11:39:42.899232+00 |             |               49

## 6 SETTINGS
              name              | setting | unit 
--------------------------------+---------+------
 autovacuum_vacuum_scale_factor | 0.2     | 
 effective_cache_size           | 100886  | 8kB
 maintenance_work_mem           | 65536   | kB
 max_connections                | 50      | 
 random_page_cost               | 4       | 
 server_version                 | 16.14   | 
 shared_buffers                 | 16384   | 8kB
 track_io_timing                | off     | 
 work_mem                       | 4096    | kB

  extname   | extversion 
------------+------------
 plpgsql    | 1.0
 btree_gist | 1.7
 pgcrypto   | 1.3
 pg_trgm    | 1.6
 dblink     | 1.2

 conns | state  
-------+--------
    27 | 
     1 | active

ROLLBACK
```
## gcloud describe
```
databaseVersion: POSTGRES_16
settings:
  availabilityType: ZONAL
  dataDiskSizeGb: '20'
  dataDiskType: PD_SSD
  databaseFlags:
  - name: cloudsql.iam_authentication
    value: on
  storageAutoResize: true
  tier: db-g1-small
```

## Top 15 problems
1. obligation_instances: 1.36M seq scans x 10.3k rows = 14.0B rows read (86k live). Hot query path missing an index/predicate match.
2. goats: 6.29M seq scans x 1.6k (whole table) = 10.3B rows.
3. weighing_observations: 1.36M full scans = 2.3B; FKs campaign_id, campaign_shed_id, proof_artifact_id unindexed.
4. feed_direction_issue_rows: 48.7k seq scans x 42k rows (half table) = 2.06B; FK feed_direction_issue_id unindexed - likely the cause.
5. obligation_batches 9.3M seq scans (1.4B); goat_shed_partitions 812k full scans (1.36B).
6. vaccination_drive_assignment_members 661M, vaccination_completions 292M (full scans of ~6k rows).
7. Tiny config tables scanned tens of millions of times (protocol_definitions 40.6M, protocol_versions 34.8M, workforce_members 12.4M, protocol_rules 6.7M, animal_stage_lookup 6.8M, locations 4.6M): N+1 lookups per request - cache in app.
8. analytics.app_events 2.6 GB, no retention, 548 full scans of ~1M rows.
9. outbox_messages 962 MB / 362k rows, 13.7% dead, 1,260 full scans x 89k; no pruning of delivered rows apparent.
10. audit_log 831 MB with 586 MB of indexes; 2 unused indexes = 99 MB (tenant_resource_recorded 56 MB, tenant_actor_recorded 43 MB); 554 full scans x 131k; FK decision_id unindexed.
11. notification_requests 628 MB, 11.4% dead, unused feed_alerts_idx 34 MB, 11.7k scans x 16.7k.
12. Redundant indexes: herd_signal_activity_windows_tag_idx 58 MB (prefix of pkey), notification_delivery_attempts_request_idx 18 MB (prefix of unique), ~28 more small prefix dups.
13. 241 indexes never used (148 MB, 100 non-unique = 140 MB).
14. ~280 FK constraints without a leading index (worst: audit_log.decision_id, notification_delivery_attempts.notification_request_id, verification_review_events.proof_id, obligation_instances trigger FK, weighing_observations x3).
15. Instance undersized/untuned: db-g1-small, shared_buffers 128 MB vs 8 GB DB, random_page_cost 4 on SSD, no pg_stat_statements/Query Insights, dead-tuple backlog on herd_signal_activity_windows (65k), domain_event_processed_events (14%), verification_items (14.9%) - scale_factor 0.2 too lax for queue tables.

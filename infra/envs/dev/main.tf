locals {
  labels = {
    app        = "goatos"
    env        = "dev"
    managed_by = "terraform"
    layer      = "foundation"
    company    = "vgoats"
  }

  base_runtime_service_accounts = {
    api = {
      account_id   = "goatos-api-dev"
      display_name = "Goat OS dev API runtime"
    }
    admin_web = {
      account_id   = "goatos-admin-web-dev"
      display_name = "Goat OS dev admin-web runtime"
    }
    # kernel_worker: consolidated long-running SERVICE replacing the retired
    # per-stage scheduled Cloud Run Jobs. See
    # docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
    kernel_worker = {
      account_id   = "goatos-kernel-worker-dev"
      display_name = "Goat OS dev kernel worker runtime"
    }
    outbox_dlq = {
      account_id   = "goatos-outbox-dlq-dev"
      display_name = "Goat OS dev outbox DLQ operator runtime"
    }
    # partition_maintainer retained until the de-partition migration (KERN-REV-02).
    partition_maintainer = {
      account_id   = "goatos-partition-maint-dev"
      display_name = "Goat OS dev partition coverage maintainer runtime"
    }
    # herd_signals_partition_maintenance: daily herd_signal_packets partition
    # ensure/prune job (backend/cmd/herd-signals-partition-maintenance, migration
    # 000201). Separate from partition_maintainer above -- that job maintains
    # unrelated monthly partitions; this one is hardcoded to herd_signal_packets
    # only. See docs/modules/herd-signals-system-design.md Section 3.2.
    herd_signals_partition_maintenance = {
      account_id   = "goatos-herd-sig-partmnt-dev"
      display_name = "Goat OS dev herd-signals partition maintenance runtime"
    }
    migrate = {
      account_id   = "goatos-migrate-dev"
      display_name = "Goat OS dev migration job runtime"
    }
    legacy_sync = {
      account_id   = "goatos-legacy-sync-dev"
      display_name = "Goat OS dev legacy sync runtime"
    }
    scheduler = {
      account_id   = "goatos-scheduler-dev"
      display_name = "Goat OS dev scheduler invoker"
    }
  }

  # KERN-01 two-phase kernel-worker cutover: the per-stage runtime SAs the gated
  # legacy jobs (infra/envs/dev/cloud_run_jobs.tf local.legacy_stage_jobs) reference
  # via service_account_key. In phase 1 (retire_legacy_stage_jobs = false) these SAs
  # must exist so each legacy job's `google_service_account.runtime[<key>]` index
  # resolves; in phase 2 (flag = true) they are removed together with the jobs,
  # leaving only kernel_worker holding the consolidated access. Only the 7 stages
  # actually restored as jobs are here — obligation_sweeper / notification_dispatcher
  # are NOT restored (their near_term_kernel Cloud Tasks queue was retired), so their
  # SAs stay removed.
  legacy_stage_runtime_service_accounts = var.retire_legacy_stage_jobs ? {} : {
    outbox_relay = {
      account_id   = "goatos-outbox-relay-dev"
      display_name = "Goat OS dev outbox relay runtime"
    }
    domain_consumer = {
      account_id   = "goatos-domain-consumer-dev"
      display_name = "Goat OS dev domain event consumer runtime"
    }
    domain_event_processed_sweeper = {
      account_id   = "goatos-domain-event-sweep-dev"
      display_name = "Goat OS dev domain processed-event retention sweeper runtime"
    }
    vaccination_generator = {
      # Base pre-convergence value was "goatos-vaccination-generator-dev" (32 chars),
      # which exceeds GCP's 30-char account_id limit and would fail apply — a latent
      # bug that never validated. Use the valid short form matching the stg sibling
      # "goatos-vax-generator-stg".
      account_id   = "goatos-vax-generator-dev"
      display_name = "Goat OS dev vaccination obligation generator runtime"
    }
    inventory_batch_reconciler = {
      account_id   = "goatos-inventory-reconcile-dev"
      display_name = "Goat OS dev inventory batch reconciler runtime"
    }
    idempotency_key_sweeper = {
      account_id   = "goatos-idempotency-sweeper-dev"
      display_name = "Goat OS dev idempotency key sweeper runtime"
    }
    sop_review_fanout_retry = {
      account_id   = "goatos-sop-review-retry-dev"
      display_name = "Goat OS dev SOP review fanout retry runtime"
    }
  }

  runtime_service_accounts = merge(
    local.base_runtime_service_accounts,
    local.legacy_stage_runtime_service_accounts,
  )

  # Keys of the legacy stage SAs present in this phase (empty in phase 2). Used to
  # gate their Cloud SQL client + database_url secret access by the same flag.
  legacy_stage_sa_keys = keys(local.legacy_stage_runtime_service_accounts)

  database_clients = toset(concat(
    [
      "api",
      "kernel_worker",
      "outbox_dlq",
      "partition_maintainer",
      "herd_signals_partition_maintenance",
      "migrate",
      "legacy_sync",
    ],
    local.legacy_stage_sa_keys,
  ))

  secret_containers = {
    database_url = {
      secret_id = "goatos-dev-database-url"
      # The consolidated kernel_worker replaces the 9 retired per-stage jobs that
      # each used to read the DB secret; keep it aligned with local.database_clients.
      # KERN-01: in phase 1 (retire_legacy_stage_jobs = false) the restored legacy
      # stage SAs are appended so their jobs can read the DB secret; in phase 2 they
      # drop out (local.legacy_stage_sa_keys is empty), leaving only kernel_worker.
      accessors = concat(
        [
          "api",
          "kernel_worker",
          "outbox_dlq",
          "partition_maintainer",
          "herd_signals_partition_maintenance",
          "migrate",
          "legacy_sync",
        ],
        local.legacy_stage_sa_keys,
      )
    }
    db_app_credential = {
      secret_id = "goatos-dev-db-app-credential"
      accessors = [
        "api",
        "migrate",
      ]
    }
    auth_issuer = {
      secret_id = "goatos-dev-auth-issuer"
      accessors = [
        "api",
      ]
    }
    auth_audience = {
      secret_id = "goatos-dev-auth-audience"
      accessors = [
        "api",
      ]
    }
    auth_jwks_url = {
      secret_id = "goatos-dev-auth-jwks-url"
      accessors = [
        "api",
      ]
    }
    firebase_web_config = {
      secret_id = "goatos-dev-firebase-web-config"
      accessors = [
        "admin_web",
      ]
    }
    admin_web_bearer_token = {
      secret_id = "goatos-dev-admin-web-bearer-token"
      accessors = [
        "admin_web",
      ]
    }
    admin_web_api_base_url = {
      secret_id = "goatos-dev-admin-web-api-base-url"
      accessors = [
        "admin_web",
      ]
    }
    admin_web_tenant_id = {
      secret_id = "goatos-dev-admin-web-tenant-id"
      accessors = [
        "admin_web",
      ]
    }
    notification_slack_webhook_url = {
      secret_id = "goatos-dev-notification-slack-webhook-url"
      accessors = [
        "kernel_worker",
      ]
    }
    notification_incident_webhook_url = {
      secret_id = "goatos-dev-notification-incident-webhook-url"
      accessors = [
        "kernel_worker",
      ]
    }
  }

  secret_accessor_bindings = merge([
    for secret_key, secret in local.secret_containers : {
      for accessor in secret.accessors :
      "${secret_key}.${accessor}" => {
        secret_key = secret_key
        accessor   = accessor
      }
    }
  ]...)
}

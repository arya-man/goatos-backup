locals {
  labels = {
    app        = "goatos"
    env        = "stg"
    managed_by = "terraform"
    layer      = "foundation"
    company    = "vgoats"
  }

  base_runtime_service_accounts = {
    api = {
      account_id   = "goatos-api-stg"
      display_name = "Goat OS staging API runtime"
    }
    admin_web = {
      account_id   = "goatos-admin-web-stg"
      display_name = "Goat OS staging admin-web runtime"
    }
    # kernel_worker: the single consolidated long-running SERVICE that replaces
    # the retired per-stage scheduled Cloud Run Jobs (outbox relay, domain
    # consumer, processed-event/idempotency/inventory/SOP sweepers, vaccination
    # generator, obligation sweeper, notification dispatcher). Its SA holds the
    # union of those stages' access (Cloud SQL, Pub/Sub publish+subscribe, FCM,
    # secrets). See docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
    kernel_worker = {
      account_id   = "goatos-kernel-worker-stg"
      display_name = "Goat OS staging kernel worker runtime"
    }
    outbox_dlq = {
      account_id   = "goatos-outbox-dlq-stg"
      display_name = "Goat OS staging outbox DLQ operator runtime"
    }
    # partition_maintainer retained until the de-partition migration removes the
    # partitioned event/history tables and this job (tracked as KERN-REV-02).
    partition_maintainer = {
      account_id   = "goatos-partition-maint-stg"
      display_name = "Goat OS staging partition coverage maintainer runtime"
    }
    migrate = {
      account_id   = "goatos-migrate-stg"
      display_name = "Goat OS staging migration job runtime"
    }
    legacy_sync = {
      account_id   = "goatos-legacy-sync-stg"
      display_name = "Goat OS staging legacy sync runtime"
    }
    scheduler = {
      account_id   = "goatos-scheduler-stg"
      display_name = "Goat OS staging scheduler invoker"
    }
  }

  # KERN-01 two-phase kernel-worker cutover: the per-stage runtime SAs the gated
  # legacy jobs (infra/envs/stg/cloud_run_jobs.tf local.legacy_stage_jobs) reference
  # via service_account_key. In phase 1 (retire_legacy_stage_jobs = false) these SAs
  # must exist so each legacy job's `google_service_account.runtime[<key>]` index
  # resolves; in phase 2 (flag = true) they are removed together with the jobs,
  # leaving only kernel_worker holding the consolidated access. Only the 7 stages
  # actually restored as jobs are here — obligation_sweeper / notification_dispatcher
  # are NOT restored (their near_term_kernel Cloud Tasks queue was retired), so their
  # SAs stay removed.
  legacy_stage_runtime_service_accounts = var.retire_legacy_stage_jobs ? {} : {
    outbox_relay = {
      account_id   = "goatos-outbox-relay-stg"
      display_name = "Goat OS staging outbox relay runtime"
    }
    domain_consumer = {
      account_id   = "goatos-domain-consumer-stg"
      display_name = "Goat OS staging domain event consumer runtime"
    }
    domain_event_processed_sweeper = {
      account_id   = "goatos-domain-event-sweep-stg"
      display_name = "Goat OS staging domain processed-event retention sweeper runtime"
    }
    vaccination_generator = {
      account_id   = "goatos-vax-generator-stg"
      display_name = "Goat OS staging vaccination obligation generator runtime"
    }
    inventory_batch_reconciler = {
      account_id   = "goatos-inventory-reconcile-stg"
      display_name = "Goat OS staging inventory batch reconciler runtime"
    }
    idempotency_key_sweeper = {
      account_id   = "goatos-idempotency-sweeper-stg"
      display_name = "Goat OS staging idempotency key sweeper runtime"
    }
    sop_review_fanout_retry = {
      account_id   = "goatos-sop-review-retry-stg"
      display_name = "Goat OS staging SOP review fanout retry runtime"
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
      "migrate",
      "legacy_sync",
    ],
    local.legacy_stage_sa_keys,
  ))

  secret_containers = {
    database_url = {
      secret_id = "goatos-stg-database-url"
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
          "migrate",
          "legacy_sync",
        ],
        local.legacy_stage_sa_keys,
      )
    }
    db_app_credential = {
      secret_id = "goatos-stg-db-app-credential"
      accessors = [
        "api",
        "migrate",
      ]
    }
    auth_issuer = {
      secret_id = "goatos-stg-auth-issuer"
      accessors = [
        "api",
      ]
    }
    auth_audience = {
      secret_id = "goatos-stg-auth-audience"
      accessors = [
        "api",
      ]
    }
    auth_jwks_url = {
      secret_id = "goatos-stg-auth-jwks-url"
      accessors = [
        "api",
      ]
    }
    auth_allowed_emails = {
      secret_id = "goatos-stg-auth-allowed-emails"
      accessors = [
        "api",
      ]
    }
    bulk_import_preview_signing_key = {
      secret_id = "goatos-stg-bulk-import-preview-signing-key"
      accessors = [
        "api",
      ]
    }
    proof_gcs_service_account_json = {
      secret_id = "goatos-stg-gcs-service-account-json"
      accessors = [
        "api",
      ]
    }
    firebase_web_config = {
      secret_id = "goatos-stg-firebase-web-config"
      accessors = [
        "admin_web",
      ]
    }
    google_oauth_web_credential = {
      secret_id = "goatos-stg-google-oauth-web-credential"
      accessors = [
        "admin_web",
      ]
    }
    admin_web_api_base_url = {
      secret_id = "goatos-stg-admin-web-api-base-url"
      accessors = [
        "admin_web",
      ]
    }
    admin_web_tenant_id = {
      secret_id = "goatos-stg-admin-web-tenant-id"
      accessors = [
        "admin_web",
      ]
    }
    notification_slack_webhook_url = {
      secret_id = "goatos-stg-notification-slack-webhook-url"
      accessors = [
        "kernel_worker",
      ]
    }
    notification_incident_webhook_url = {
      secret_id = "goatos-stg-notification-incident-webhook-url"
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

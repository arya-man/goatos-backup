locals {
  labels = {
    app        = "goatos"
    env        = "stg"
    managed_by = "terraform"
    layer      = "foundation"
    company    = "vgoats"
  }

  runtime_service_accounts = {
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

  database_clients = toset([
    "api",
    "kernel_worker",
    "outbox_dlq",
    "partition_maintainer",
    "migrate",
    "legacy_sync",
  ])

  secret_containers = {
    database_url = {
      secret_id = "goatos-stg-database-url"
      accessors = [
        "api",
        "outbox_relay",
        "outbox_dlq",
        "domain_consumer",
        "domain_event_processed_sweeper",
        "vaccination_generator",
        "obligation_sweeper",
        "notification_dispatcher",
        "inventory_batch_reconciler",
        "idempotency_key_sweeper",
        "sop_review_fanout_retry",
        "partition_maintainer",
        "migrate",
        "legacy_sync",
      ]
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
        "notification_dispatcher",
      ]
    }
    notification_incident_webhook_url = {
      secret_id = "goatos-stg-notification-incident-webhook-url"
      accessors = [
        "notification_dispatcher",
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

locals {
  labels = {
    app        = "goatos"
    env        = "dev"
    managed_by = "terraform"
    layer      = "foundation"
    company    = "vgoats"
  }

  runtime_service_accounts = {
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
      secret_id = "goatos-dev-database-url"
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
        "notification_dispatcher",
      ]
    }
    notification_incident_webhook_url = {
      secret_id = "goatos-dev-notification-incident-webhook-url"
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

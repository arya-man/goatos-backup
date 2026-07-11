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
    outbox_relay = {
      account_id   = "goatos-outbox-relay-stg"
      display_name = "Goat OS staging outbox relay runtime"
    }
    outbox_dlq = {
      account_id   = "goatos-outbox-dlq-stg"
      display_name = "Goat OS staging outbox DLQ operator runtime"
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
      account_id   = "goatos-vaccination-generator-stg"
      display_name = "Goat OS staging vaccination obligation generator runtime"
    }
    obligation_sweeper = {
      account_id   = "goatos-obligation-sweeper-stg"
      display_name = "Goat OS staging obligation sweeper runtime"
    }
    calendar_projector = {
      account_id   = "goatos-calendar-projector-stg"
      display_name = "Goat OS staging calendar projector runtime"
    }
    calendar_reminder_sweeper = {
      account_id   = "goatos-calendar-reminder-stg"
      display_name = "Goat OS staging calendar reminder sweeper runtime"
    }
    calendar_escalation_sweeper = {
      account_id   = "goatos-calendar-escalation-stg"
      display_name = "Goat OS staging calendar escalation sweeper runtime"
    }
    notification_dispatcher = {
      account_id   = "goatos-notification-dispatcher-stg"
      display_name = "Goat OS staging notification dispatcher runtime"
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
    cloud_tasks_enqueuer = {
      account_id   = "goatos-cloud-tasks-stg"
      display_name = "Goat OS staging Cloud Tasks OAuth enqueuer"
    }
  }

  database_clients = toset([
    "api",
    "outbox_relay",
    "outbox_dlq",
    "domain_consumer",
    "domain_event_processed_sweeper",
    "vaccination_generator",
    "obligation_sweeper",
    "calendar_projector",
    "calendar_reminder_sweeper",
    "calendar_escalation_sweeper",
    "notification_dispatcher",
    "inventory_batch_reconciler",
    "idempotency_key_sweeper",
    "sop_review_fanout_retry",
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
        "calendar_projector",
        "calendar_reminder_sweeper",
        "calendar_escalation_sweeper",
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
    auth_session_allowed_tenant_ids = {
      secret_id = "goatos-stg-auth-session-allowed-tenant-ids"
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
    appcheck_enforce = {
      secret_id = "goatos-stg-appcheck-enforce"
      accessors = [
        "api",
      ]
    }
    appcheck_issuer = {
      secret_id = "goatos-stg-appcheck-issuer"
      accessors = [
        "api",
      ]
    }
    appcheck_audience = {
      secret_id = "goatos-stg-appcheck-audience"
      accessors = [
        "api",
      ]
    }
    appcheck_jwks_url = {
      secret_id = "goatos-stg-appcheck-jwks-url"
      accessors = [
        "api",
      ]
    }
    proof_gcs_service_account_json = {
      secret_id = "goatos-stg-proof-gcs-service-account-json"
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

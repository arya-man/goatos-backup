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
    outbox_relay = {
      account_id   = "goatos-outbox-relay-dev"
      display_name = "Goat OS dev outbox relay runtime"
    }
    domain_consumer = {
      account_id   = "goatos-domain-consumer-dev"
      display_name = "Goat OS dev domain event consumer runtime"
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
    "outbox_relay",
    "domain_consumer",
    "migrate",
    "legacy_sync",
  ])

  secret_containers = {
    database_url = {
      secret_id = "goatos-dev-database-url"
      accessors = [
        "api",
        "outbox_relay",
        "domain_consumer",
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

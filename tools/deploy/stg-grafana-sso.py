#!/usr/bin/env python3
"""Bounded Grafana 11.3 Google OAuth config; never handles OAuth secret values.

Source contract: grafana/grafana v11.3.0 generic_oauth.go UserInfo and
social_base.go extractRoleAndAdminOptional evaluate raw token/userinfo JSON,
then reject missing roles when role_attribute_strict is enabled.
"""
import copy
import json
import subprocess
import sys
from urllib.parse import parse_qs, urlparse

EMAILS = ("ravi@mesha.sg", "manohark@mesha.sg", "manju@mesha.sg", "aryaman@mesha.sg")
ROLE = "email_verified == `true` && contains(`" + json.dumps(EMAILS, separators=(",", ":")) + "`, email) && 'Viewer' || ''"
ENV = {
    "GF_AUTH_GENERIC_OAUTH_ENABLED": "true",
    "GF_AUTH_GENERIC_OAUTH_NAME": "Google",
    "GF_AUTH_GENERIC_OAUTH_ALLOW_SIGN_UP": "true",
    "GF_AUTH_GENERIC_OAUTH_AUTO_LOGIN": "false",
    "GF_AUTH_GENERIC_OAUTH_SCOPES": "openid email profile",
    "GF_AUTH_GENERIC_OAUTH_AUTH_URL": "https://accounts.google.com/o/oauth2/v2/auth",
    "GF_AUTH_GENERIC_OAUTH_TOKEN_URL": "https://oauth2.googleapis.com/token",
    "GF_AUTH_GENERIC_OAUTH_API_URL": "https://openidconnect.googleapis.com/v1/userinfo",
    "GF_AUTH_GENERIC_OAUTH_EMAIL_ATTRIBUTE_PATH": "email",
    "GF_AUTH_GENERIC_OAUTH_LOGIN_ATTRIBUTE_PATH": "email",
    "GF_AUTH_GENERIC_OAUTH_ROLE_ATTRIBUTE_PATH": ROLE,
    "GF_AUTH_GENERIC_OAUTH_ROLE_ATTRIBUTE_STRICT": "true",
    "GF_AUTH_GENERIC_OAUTH_SKIP_ORG_ROLE_SYNC": "false",
    "GF_AUTH_GENERIC_OAUTH_ALLOW_ASSIGN_GRAFANA_ADMIN": "false",
    "GF_AUTH_GENERIC_OAUTH_ALLOWED_DOMAINS": "mesha.sg",
    "GF_AUTH_GENERIC_OAUTH_ORG_ATTRIBUTE_PATH": "",
    "GF_AUTH_GENERIC_OAUTH_ORG_MAPPING": "",
    "GF_AUTH_GENERIC_OAUTH_USE_PKCE": "true",
    "GF_AUTH_BASIC_ENABLED": "true",
}
SECRETS = {
    "GF_AUTH_GENERIC_OAUTH_CLIENT_ID": "goatos-stg-grafana-oauth-client-id",
    "GF_AUTH_GENERIC_OAUTH_CLIENT_SECRET": "goatos-stg-grafana-oauth-client-secret",
}

def container(service):
    containers = service["spec"]["template"]["spec"]["containers"]
    named = [c for c in containers if c.get("name") == "grafana"]
    if len(named) == 1:
        return named[0]
    if len(containers) == 1 and not containers[0].get("name"):
        return containers[0]
    raise ValueError("Cannot identify Grafana container")


def preflight(service):
    c = container(service)
    e = {v["name"]: v for v in c.get("env", [])}
    assert service["metadata"]["annotations"]["run.googleapis.com/ingress"] == "internal-and-cloud-load-balancing", "Grafana must remain LB-only"
    for key, value in {"GF_AUTH_ANONYMOUS_ENABLED": "false", "GF_USERS_ALLOW_SIGN_UP": "false", "GF_SERVER_ROOT_URL": "https://grafana.mesha.sg/"}.items():
        assert e.get(key, {}).get("value") == value, f"Unsafe/missing {key}"
    assert e.get("GF_AUTH_BASIC_ENABLED", {"value": "true"}).get("value") == "true", "Basic smoke authentication must remain enabled"
    assert e.get("GF_SECURITY_ADMIN_PASSWORD", {}).get("valueFrom", {}).get("secretKeyRef"), "Admin password must remain secret-backed"
    return c


def update(service):
    c = preflight(service)
    args = ["gcloud", "run", "services", "update", "goatos-stg-grafana", "--project=goatos-stg", "--region=asia-south1", "--no-traffic", "--quiet"]
    if c.get("name"):
        args.append("--container=" + c["name"])
    # An alternate delimiter preserves commas inside the JMESPath JSON literal.
    args.append("--update-env-vars=^~^" + "~".join(k + "=" + v for k, v in ENV.items()))
    args.append("--update-secrets=" + ",".join(k + "=" + v + ":latest" for k, v in SECRETS.items()))
    subprocess.run(args, check=True)


def verify(before, after):
    preflight(before)
    c = preflight(after)
    env = {v["name"]: v for v in c.get("env", [])}
    for key, value in ENV.items():
        assert key in env and env[key].get("value", "") == value, f"SSO readback mismatch: {key}"
    for key, secret in SECRETS.items():
        ref = env.get(key, {}).get("valueFrom", {}).get("secretKeyRef", {})
        assert ref.get("name") == secret and ref.get("key") == "latest", f"SSO secret reference mismatch: {key}"
    def preserved(service):
        spec = copy.deepcopy(service["spec"]["template"]["spec"])
        for item in spec["containers"]:
            if item.get("name") == c.get("name"):
                item["env"] = sorted([v for v in item.get("env", []) if v["name"] not in ENV and v["name"] not in SECRETS], key=lambda v: v["name"])
        return spec
    assert preserved(before) == preserved(after), "SSO update changed unrelated runtime configuration"


def verify_redirect(headers):
    locations = [line.split(":", 1)[1].strip() for line in headers.splitlines() if line.lower().startswith("location:")]
    assert len(locations) == 1, "OAuth start must redirect once"
    url = urlparse(locations[0]); query = parse_qs(url.query)
    assert url.scheme == "https" and url.netloc == "accounts.google.com" and url.path == "/o/oauth2/v2/auth", "Unexpected OAuth provider"
    assert query.get("redirect_uri") == ["https://grafana.mesha.sg/login/generic_oauth"], "Wrong OAuth callback"
    assert query.get("state", [""])[0], "OAuth state missing"
    assert query.get("client_id", [""])[0], "OAuth client missing"
    assert query.get("code_challenge_method") == ["S256"] and query.get("code_challenge", [""])[0], "OAuth PKCE missing"


if __name__ == "__main__":
    command, *paths = sys.argv[1:]
    if command == "redirect":
        verify_redirect(open(paths[0]).read())
    else:
        services = [json.load(open(p)) for p in paths]
        {"preflight": preflight, "update": update, "verify": verify}[command](*services)

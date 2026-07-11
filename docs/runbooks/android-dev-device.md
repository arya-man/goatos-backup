# Run the Goat OS Android app on a device against the local laptop backend

One command, no re-figuring:

```bash
make android-dev-run                       # auto-picks the connected device
# or:
tools/dev/android-dev-run.sh -s <serial>   # target a specific adb serial
tools/dev/android-dev-run.sh --token-only  # just re-mint + bake a fresh token
tools/dev/android-dev-run.sh --no-clear    # keep app data (skip pm clear)
```

## Prerequisites

1. Local backend up on `:8080` — `make dev-local-service-start` (check: `curl -s -o /dev/null -w '%{http_code}' localhost:8080/readyz` → `204`).
2. Device connected by **USB** with **Developer Options → USB debugging** on; tap **Allow** on the RSA prompt. Works on a physical phone or the emulator.
3. JDK 21 available (the script finds `openjdk@21` / `/usr/libexec/java_home -v 21`).

## What the script does (and why each step exists)

1. **Mints a fresh dev bearer token and VALIDATES it** against `GET /app/bootstrap` (must be `200`) *before* building. The dev flavor authenticates with an HS256 bearer baked at build time (`BuildConfig.DEV_BEARER_TOKEN` ← gradle prop `goatosDevBearerToken`). Two things make this the usual failure:
   - The token **expires** (≤ 24h, capped by `GOATOS_AUTH_MAX_TOKEN_TTL`). A day later → silent `401` → the app shows **"Couldn't load your workspace."**
   - It must be signed with the **secret the RUNNING backend actually uses**, which is **not always** the `run-local-stack-supervised.sh` default (the stack can be started with an env override). The script tries, in order, `$GOATOS_AUTH_HS256_SECRET` → the **live `:8080` process env** → the supervised-script default, and keeps the first token that returns `200`.
2. **Builds** `:app:assembleDevDebug` (rebakes the fresh token).
3. **Installs** `-r`, then **`pm clear`** (so the app drops the old cached token in DataStore and picks up the freshly-baked one — `install -r` alone keeps app data).
4. **`adb reverse tcp:8080 tcp:8080`** — tunnels the device's `localhost:8080` to the laptop over USB. The dev flavor's `API_BASE_URL` is `http://localhost:8080/` for exactly this reason. **`10.0.2.2` is emulator-only and does NOT reach the laptop from a physical phone.**
5. **Launches** `MainActivity`.

## Verifying

Watch the backend access log for the device's bootstrap call:

```bash
tail -f .codex-goatos-render/logs/local-api.log | grep /app/bootstrap
# expect: "path":"/app/bootstrap","status":200
```

`401` there = token expired / wrong secret (re-run the script; it re-mints). No `/app/bootstrap` line at all = the tunnel isn't up (`adb reverse --list` should show `tcp:8080 tcp:8080`; re-run the script).

## Notes

- `goatosDevBearerToken` lives in `~/.gradle/gradle.properties` (git-ignored, machine-local) — never committed. The script rewrites just that line; the token is never printed.
- Cleartext HTTP to `localhost`/`127.0.0.1`/`10.0.2.2` is already allowed by `app/src/main/res/xml/network_security_config.xml`; prod stays HTTPS-only.
- stg/prod flavors are unaffected — they point at the deployed HTTPS API and use Firebase auth, not the dev bearer.

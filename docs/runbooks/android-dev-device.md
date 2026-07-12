# Run the Goat OS Android app on a device against the local laptop backend

One command, no re-figuring:

```bash
make android-doctor                        # JDK + SDK + AVD + Gradle diagnosis
make android-dev-run                       # USB phone, else auto-starts emulator
# or:
make android-emulator-ensure               # explicitly start/wait for the AVD
tools/dev/android-dev-run.sh -s <serial>   # target a specific adb serial
tools/dev/android-dev-run.sh --token-only  # just re-mint + bake a fresh token
tools/dev/android-dev-run.sh --no-clear    # keep app data (skip pm clear)
```

## Prerequisites

1. Local backend up on `:8080` — `make dev-local-service-start` (check: `curl -s -o /dev/null -w '%{http_code}' localhost:8080/readyz` → `204`).
2. Either a physical device connected by **USB** with USB debugging authorized,
   or one AVD configured in Android Studio Device Manager. A missing USB phone is
   not a blocker: `android-dev-run` starts and waits for the AVD automatically.
3. JDK 21 available. Repository scripts resolve Homebrew `openjdk@21` themselves;
   they do not depend on the current shell having `JAVA_HOME` set.

## One-time permanent macOS shell setup

Repository commands work without shell configuration. For direct `java`, `adb`,
`emulator`, and `./gradlew` commands in every new terminal, use this machine-local
file and source it from both `~/.zprofile` and `~/.zshrc`:

```bash
mkdir -p ~/.config/goatos
cat > ~/.config/goatos/android-env.zsh <<'EOF'
export JAVA_HOME="/opt/homebrew/opt/openjdk@21"
export ANDROID_HOME="$HOME/Library/Android/sdk"
export ANDROID_SDK_ROOT="$ANDROID_HOME"
export PATH="$JAVA_HOME/bin:$ANDROID_HOME/platform-tools:$ANDROID_HOME/emulator:$ANDROID_HOME/cmdline-tools/latest/bin:$PATH"
EOF
```

Add this one line to both shell files:

```bash
[ -f "$HOME/.config/goatos/android-env.zsh" ] && source "$HOME/.config/goatos/android-env.zsh"
```

Open a new terminal and run `make android-doctor`. Goat OS uses JDK 21 to run
Gradle/AGP while compiling Java/Kotlin bytecode for target 17; those are not a
contradiction.

## When no USB phone is available

`make android-dev-run` checks for an authorized physical phone first, then a
booted emulator, then starts `${GOATOS_ANDROID_AVD}` (or the first configured
AVD). It waits for `sys.boot_completed=1` before building/installing.

```bash
GOATOS_ANDROID_AVD=Medium_Phone_API_36.1 make android-emulator-ensure
GOATOS_ANDROID_EMULATOR_HEADLESS=1 make android-emulator-ensure # CI-style
```

If no AVD exists: Android Studio → Device Manager → Create Device → choose a
medium phone → install an ARM64 API 36 image. Then rerun `make android-doctor`.

## What the script does (and why each step exists)

1. **Mints a fresh dev bearer token and VALIDATES it** against `GET /app/bootstrap` (must be `200`) *before* building. The dev flavor authenticates with an HS256 bearer baked at build time (`BuildConfig.DEV_BEARER_TOKEN` ← gradle prop `goatosDevBearerToken`). Two things make this the usual failure:
   - The token **expires** (≤ 24h, capped by `GOATOS_AUTH_MAX_TOKEN_TTL`). A day later → silent `401` → the app shows **"Couldn't load your workspace."**
   - It must be signed with the **secret the RUNNING backend actually uses**, which is **not always** the `run-local-stack-supervised.sh` default (the stack can be started with an env override). The script tries, in order, `$GOATOS_AUTH_HS256_SECRET` → the **live `:8080` process env** → the supervised-script default, and keeps the first token that returns `200`.
2. Selects an authorized USB device or boots the emulator fallback, then
   **builds** `:app:assembleDevDebug` (rebakes the fresh token).
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

## Auth in dev is the LOCAL bearer — NEVER Firebase (do not re-derive this)

The dev flavor authenticates with the baked HS256 bearer, NOT Firebase. `make android-dev-run` mints
a token for the target user and bakes it into `BuildConfig.DEV_BEARER_TOKEN`. Mechanics (from code):
- `SessionViewModel.signInWith{Email,Google}` short-circuit to `signInWithDevToken()` in the dev
  flavor — **the email/password/Google are IGNORED; it writes the baked bearer into `SessionStore`**
  (`SessionViewModel.kt`, `authMode == DEV_BEARER`).
- The network layer reads the token from `SessionStore.currentToken()` in dev (`AppModule` `tokenProvider`,
  `if (FLAVOR=="dev")`), so once signed in every request carries the baked bearer.

**So if the dev app shows the login screen, you are NOT blocked — just TAP "Sign in" (or "Continue
with Google") and you're in as the baked user against local `:8080`.** Do NOT type real
Google/work-email credentials: in dev they're ignored, and doing so on a stg/prod build hits the
deployed API + `goatos-prod`, never local. (Some builds auto-sign-in on boot with no screen; if a
build STOPS auto-signing-in, that's a minor UX regression — the tap-through still works. A build that
genuinely demands real Firebase creds to proceed in dev IS a regression: restore the dev-token path.)

## Testing as a specific ROLE / switching roles (operator ↔ director ↔ CEO …)

There is NO in-app role/user picker in dev — one build = one baked identity. To be a role, mint the
bearer for a user that has that role's grant, then run. Role comes from `user_scope_grants`.

**Two steps, per role:**

```bash
# 1) grant a (dev) user UUID a role — one-time per uuid+role. DATABASE_URL = the local Cloud-SQL-proxy
#    DSN the running :8080 uses (pull it: ps eww <:8080 pid> | tr ' ' '\n' | grep ^DATABASE_URL=).
#    GOATOS_ENV=local is required (seed-dev-grant refuses non-local targets).
cd backend
DATABASE_URL='postgres://postgres:<pw>@127.0.0.1:55432/goatos?sslmode=disable' GOATOS_ENV=local \
  go run ./cmd/seed-dev-grant \
    -tenant-id 00000000-0000-4000-8000-000000000001 \
    -user-id 90000000-0000-4000-8000-000000000103 \
    -role pc_director \
    -department preventive_care   # provisions a workforce_member so department-driven nav works

# 2) build + deploy the app AS that user (overrides the default identity, then the normal script):
GOATOS_LOCAL_USER_ID=90000000-0000-4000-8000-000000000103 make android-dev-run
```

Switch role = repeat step 2 with a different `GOATOS_LOCAL_USER_ID` (grant it once via step 1 first).

**Canonical dev identities (convention — any UUID works once granted):**

| role (`-role`) | suggested user-id | can do |
|---|---|---|
| `operator` | `…000101` (the script default) | EXECUTE a drive: scan → submit + proof. Cannot close/verify. |
| `park_head` | `…000102` | leadership follow-up in app; close + post verification on admin-web |
| `pc_director` | `…000103` | leadership follow-up in app; close + post verification on admin-web |
| `ceo_internal` | `…000104` | leadership follow-up in app; close + post verification on admin-web |
| `verifier` | `…000105` | leadership follow-up in app; post verification on admin-web |

(`…` = `90000000-0000-4000-8000-0000000001`.) The current business rule: **Director / CEO / CxO / Park Head /
verifier can close + verify through admin-web; operators execute only.** On mobile today, leadership is
follow-up/read-only (plus assign where granted); the operator path is app-only execution.

Valid roles (from `seed-dev-grant`): `admin`, `verifier`, `park_head`, `pc_director`, `operator`,
`ceo_internal`.

## Emulator stability (why it ANRs) + recording

- **System-level ANRs** ("Process system / System UI isn't responding") mean the HOST is thrashing,
  not the app — usually a concurrent `gradle` build, the backend, and a cold emulator competing.
  Run ONE build at a time; let the emulator finish booting before `android-dev-run`; don't run a
  parallel `assembleDevDebug`/agent while testing.
- Record the whole flow: `adb -s emulator-5554 shell screenrecord /sdcard/e2e.mp4` (Ctrl-C to stop),
  then `adb -s emulator-5554 pull /sdcard/e2e.mp4`. Stills: `adb -s <serial> exec-out screencap -p > shot.png`.
- Proof capture needs the camera: `adb -s <serial> shell pm grant sg.mesha.goatos.dev android.permission.CAMERA`
  and set the emulator camera to the virtual scene (AVD → Camera → VirtualScene) so proof photos/video capture works.

## Notes

- `goatosDevBearerToken` lives in `~/.gradle/gradle.properties` (git-ignored, machine-local) — never committed. The script rewrites just that line; the token is never printed.
- Cleartext HTTP to `localhost`/`127.0.0.1`/`10.0.2.2` is already allowed by `app/src/main/res/xml/network_security_config.xml`; prod stays HTTPS-only.
- stg/prod flavors are unaffected — they point at the deployed HTTPS API and use Firebase auth, not the dev bearer.

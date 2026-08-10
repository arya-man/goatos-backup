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
4. **No `google-services.json` setup needed for compile/CI.** `google-services.json`
   is a per-machine secret path (gitignored, `apps/goatos-android/.gitignore`), but
   `app/src/stg/google-services.json` and `app/src/dev/google-services.json` are
   force-committed, obviously-fake **placeholders** — see
   `apps/goatos-android/app/src/google-services-README.md`. They exist only so the
   `google-services`/Crashlytics/Perf Gradle plugins have a file to process, so a
   fresh clone can run `make ci-local JOB=android`
   (`:app:compileStgReleaseKotlin` + `:app:testStgReleaseUnitTest`) and
   `make android-dev-run` (`:app:assembleDevDebug`) without the real secret.
   `TELEMETRY_ENABLED` is `false` for `dev`/`prod` by default and the placeholder
   values never need to work at runtime. To get REAL Crashlytics/Perf/Auth
   telemetry for a flavor, download that flavor's real `google-services.json` from
   the Firebase console and drop it in at the same path with
   `git add -f apps/goatos-android/app/src/<flavor>/google-services.json`
   (the gitignore rule blocks a plain `git add`) — never hand-edit the placeholder's
   fake values in place.

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

Golden rule: a ground operator is always single-park, even in throwaway/dev data.
Do not seed one operator user with CBE and CPT visibility. If you need to compare
parks, seed two operator users (one per park) or use a director/CEO oversight
user. Seeing multiple park chips/tasks on a ground-operator phone is a bad seed
or a backend scope bug, not an acceptable demo shortcut.

**Two steps, per role:**

```bash
# 1) grant a (dev) user UUID a role — one-time per uuid+role. DATABASE_URL = the local Cloud-SQL-proxy
#    DSN the running :8080 uses (pull it: ps eww <:8080 pid> | tr ' ' '\n' | grep ^DATABASE_URL=).
#    GOATOS_ENV=local is required (seed-dev-grant refuses non-local targets).
cd backend
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable' GOATOS_ENV=local \
  go run ./cmd/seed-dev-grant \
    -tenant-id 00000000-0000-4000-8000-000000000001 \
    -user-id 90000000-0000-4000-8000-000000000103 \
    -role pc_director \
    -department preventive_care   # provisions a workforce_member so department-driven nav works

# 2) build + deploy the app AS that user (overrides the default identity, then the normal script):
GOATOS_LOCAL_USER_ID=90000000-0000-4000-8000-000000000103 make android-dev-run
```

Switch role = repeat step 2 with a different `GOATOS_LOCAL_USER_ID` (grant it once via step 1 first).

### Granting MODULES to a dev identity (`-modules`)

Role alone no longer decides what the app shows. The bottom bar and the module drawer are
composed from **module grants** (`department_module_grants`, mig `000002`;
`docs/decisions/role-module-nav-composition.md`), resolved
`user → workforce_members.department_id → module keys`. **The grant hangs off the
department, not the user** — so `-modules` only does something alongside `-department`,
and it grants those modules to *everyone* in that department.

`-modules` defaults to `vaccination`, so the command above is unchanged and still yields a
working vaccination identity. Pass it explicitly to get more:

```bash
cd backend
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable' GOATOS_ENV=local \
  go run ./cmd/seed-dev-grant \
    -tenant-id 00000000-0000-4000-8000-000000000001 \
    -user-id 90000000-0000-4000-8000-000000000101 \
    -role operator \
    -department preventive_care \
    -modules vaccination,counts     # two available modules => drawer + a switchable bar
```

- **One available module** → bottom bar only, no drawer. **Two or more** → expanded drawer
  (`navChromeFor` counts registry-known, `available`, granted modules).
- The bar is **module-scoped**: each module carries its own `NavItems`, and picking a module
  in the drawer swaps the bar. It is not a union of every granted module's tabs.
- Valid keys are whatever `moduleNavRegistry`
  (`backend/internal/workforce/app/bootstrap_copy.go`) knows: `vaccination`, `counts`, and
  the declared-but-unbuilt `feed_direction` / `breeding`. An unknown key is stored happily
  and simply contributes no nav — deliberate, so adding a module stays a registry entry plus
  a grant row rather than a migration.
- `"soon"` modules (`feed_direction`, `breeding`) show as disabled drawer rows for everyone
  regardless of grants; granting one confers no access and does not count toward the drawer
  threshold.
- Idempotent: re-running upserts on `(tenant_id, department_id, module_key)` and reactivates
  an inactive row rather than duplicating it.
- `-modules ""` skips module granting entirely (department attachment only).

**Empty bottom bar after sign-in** is almost always this: the user has a role grant and a
workforce member, but the member's department has no `department_module_grants` row. Check:

```sql
SELECT d.code, dmg.module_key, dmg.status
FROM workforce_members wm
JOIN departments d ON d.tenant_id = wm.tenant_id AND d.department_id = wm.department_id
LEFT JOIN department_module_grants dmg
  ON dmg.tenant_id = wm.tenant_id AND dmg.department_id = wm.department_id
WHERE wm.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND wm.user_id = '90000000-0000-4000-8000-000000000101'
  AND wm.status = 'active';
```

The full source seed (`make seed-vaccination-source-full`) grants department defaults on its
own via `backend/cmd/seed-roster-real` — `preventive_care` → vaccination + counts, `health` →
counts, plus the "soon" `feed`/`breeding` rows — so a fresh source seed yields working nav
without this step. `-modules` is for hand-seeded dev identities that never went through the
roster import.

**Canonical dev identities (convention — any UUID works once granted):**

| role (`-role`) | suggested user-id | can do | Counts module |
|---|---|---|---|
| `operator` | `…000101` (the script default) | EXECUTE a drive in exactly one assigned park: scan → submit + proof. RECORD birth + death in Counts for that same park. Cannot close/verify a drive and must never see another park's ground work. | capture only — Birth/Death + Shifting, **no census** |
| `park_head` | `…000102` | leadership follow-up in app; close + post verification on admin-web | capture only — Birth/Death + Shifting, **no census** |
| `pc_director` | `…000103` | leadership follow-up in app; close + post verification on admin-web | **none — module not in drawer** |
| `ceo_internal` | `…000104` | leadership follow-up in app; close + post verification on admin-web | full — census + Birth/Death + Shifting |
| `verifier` | `…000105` | post verification on admin-web. **Cannot use the mobile app at all** (see below) | **none — module not in drawer** |

(`…` = `90000000-0000-4000-8000-0000000001`.) The current business rule: **Director / CEO / CxO / Park Head /
verifier can close + verify vaccination drives through admin-web. Operators do not close or verify —
but they are no longer execution-only: a field operator RECORDS BIRTH and DEATH from the mobile app**
(maintainer-approved, Counts module → Birth/Death). On mobile today, leadership is follow-up/read-only
(plus assign where granted); the operator path is app execution plus Counts birth/death capture.

### Counts access per role (maintainer decision 2026-07-18)

The Counts module's three pages are gated **individually**, so one module shows a
different bar to different jobs. The distinction being enforced: **field capture and
tenant-wide census visibility are different authorities.** An Operator or Park Head
records births/deaths/shiftings as their own ground truth but does **not** get a
tenant-wide population view; CEO/CXO does.

| role | census `/counts` | `/counts/birth-death` | `/counts/shifting` | module in drawer |
|---|---|---|---|---|
| `operator` | NO | yes | yes | yes |
| `park_head` | NO | yes | yes | yes |
| `ceo_internal` | yes | yes | yes | yes |
| `pc_director` | NO | NO | NO | **NO — excluded entirely** |
| `verifier` | NO | NO | NO | **NO — excluded entirely** |

- The census page requires **`counts.read`** (`ceo_internal` only); the two capture pages
  require **`counts.write`**. `counts.read` is split off `goat.read` on purpose —
  `goat.read` is held by nearly every role, so reusing it would have made the census
  effectively public.
- **This is real enforcement, not just a hidden tab.** `GET /counts/breakdown` and
  `GET /herd-register/summary` require `counts.read`
  (`backend/internal/permissions/routes.go`), so a hidden census page returns **403**
  if requested directly.
- Operator and Park Head **land on `/counts/birth-death`**, not `/counts` — the
  module's landing href falls back to the first page the principal may actually open,
  so nobody arrives on a route that 403s.
- `pc_director` and `verifier` hold neither counts permission, so the module has zero
  permitted items and is **omitted from the drawer entirely** — do not expect an empty
  Counts row when testing those identities.
- Pinned by `TestCountsModuleRoleMatrix`
  (`backend/internal/workforce/app/service_test.go`); the route denials are pinned in
  `backend/internal/permissions/permissions_test.go`.

**To create a counts-capable dev identity**, grant the module to the department and use
a role that holds a counts permission:

```bash
cd backend
DATABASE_URL='postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable' GOATOS_ENV=local \
  go run ./cmd/seed-dev-grant \
    -tenant-id 00000000-0000-4000-8000-000000000001 \
    -user-id 90000000-0000-4000-8000-000000000101 \
    -role operator \
    -department preventive_care \
    -modules vaccination,counts
```

That yields a two-module drawer, with Counts showing the **two capture tabs only**. Swap
`-role operator` for `-role ceo_internal` to see the three-tab version including census.
Remember `-modules` grants to the **department**, so everyone in `preventive_care` gets
Counts — but each person still sees only the pages their role permits.

> **`verifier` cannot use the mobile app at all.** `RoleVerifier` does not hold
> `app.bootstrap`, so `GET /app/bootstrap` returns **403** and the app cannot load for
> that identity — there is no nav to inspect. This is **pre-existing** and not part of
> the 2026-07-18 Counts decision; it simply means a verifier's exclusion from Counts is
> enforced twice over. Use admin-web for verifier testing.

Scope of the operator lifecycle write, precisely:
- **Gained**: birth and death recording in the **Counts** module (`/counts/birth-death`).
- **Unchanged**: operators still cannot close a vaccination drive, post verification, approve
  proof, or run any admin bulk-status path.
- Death recording does **not** relax the critical-action guardrail. A death exit still goes through
  the dedicated `dead` + `died` guardrail route (`POST /admin/goats/{goat_id}/critical-death-exit`),
  never the regular exit primitive and never the bulk-status path — see
  `docs/features/critical-animal-action-guardrails.md`. Being reachable by an operator changes WHO
  may call it, not WHAT it enforces.

How it is wired (so you test the right route): the operator write goes through the **app** tier,
not the admin API — `POST /app/counts/{shifting,birth,death}-events`
(`backend/internal/counts/adapters/http/app_write_handler.go`), gated on the dedicated
`counts.write` permission. That permission is deliberately NOT `goat.write_identity` /
`goat.write_health`: reusing those would have handed operators every `/admin/goats/*` route.
Birth delegates to identity's `CreateAdminGoat` and death to `CriticalDeathExit`, so the domain
rules and the guardrail are reused, never re-implemented. `counts.write` is held by the ground
capture roles — flat `operator` and `park_head`, plus the Assistant Manager and Manager tiers —
and by `ceo_internal` for oversight. It is held by **neither `verifier`** (which keeps
capture and verification separate) **nor `pc_director` / the Head and Director tiers**, which act
on verified work rather than capturing it. `pc_director` previously held `counts.write` and lost
it in the 2026-07-18 decision above.

Valid flat roles (from `seed-dev-grant`): `verifier`, `park_head`, `pc_director`,
`growth_director`, `operator`, `ceo_internal`. There is no separate grantable
full-access alias; CEO/CXO/full access is `ceo_internal`.

For phone QA that needs both parks, both modules, and physical RFID scans, use
`docs/runbooks/phone-qa-throwaway-rbac.md`. That runbook deliberately uses a
throwaway database on `127.0.0.1:15544`; do not run those scan-role fixtures
against the canonical `127.0.0.1:5433` local app DB.

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

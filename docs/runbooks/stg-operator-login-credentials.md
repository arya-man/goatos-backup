# STG Operator Login Credentials

> **Canonical STG personnel rule:** `docs/runbooks/stg-login-seed-contract.md`.
> This file carries the field/verifier credential detail plus live STG weighing
> access additions. The core contract (4 SSO leadership + 4 field
> + 1 verifier) lives in the canonical runbook.

Current live RBAC roles are documented in
`docs/runbooks/current-active-rbac-roles.md`. Do not grant dormant catalog roles
such as `director_preventive_care` or `director_breeding` in STG until backend
permissions, Android role handling, seed docs, and tests are updated together.

## Canonical STG Personnel Rule + Weighing Addendum

Current documented STG access roster includes
**4 Mesha leadership (Google SSO **and** Firebase email/password,
`ceo_internal`, NO vaccination capacity)** + **4 vaccination/weighing operators
(Firebase email/password)** + **1 preventive-care director** + **1 verifier** +
**2 weighing-only operators** + **1 director execution user**.

The core seed command still materializes the leadership,
vaccination operator, director, and verifier rows. The weighing addendum rows
must be kept in sync with their Firebase Auth user, backend grant/profile, App
Distribution tester access, and assigned weighing campaign sheds.

> **Maintainer decision 2026-07-24:** leadership is no longer SSO-only. The 4
> leadership accounts now also have Firebase email/password logins (in addition
> to Google SSO). The prior "NO password / SSO-only" leadership rule is retired.

Leadership passwords (in addition to SSO):

| Person | Firebase password | Role | Adds vaccination capacity? |
|---|---|---|---|
| Ravi | `Ravi@2026` | ceo_internal | **no** |
| Manohar (Manohark) | `Manohar@2026` | ceo_internal | **no** |
| Manju | `Manju@2026` | ceo_internal | **no** |
| Aryaman | `Aryaman@2026` | ceo_internal | **no** |

Field roles and vaccination capacity:

| Person | Firebase password | Role | Department | Modules visible in STG | Adds vaccination capacity? |
|---|---|---|---|---|---|
| Amit Kumar | `Amit@2026` | operator | Preventive Care | Vaccination, Weighing | **yes** |
| Darshan Talwar | `Darshan@2026` | operator, default vaccination operator | Preventive Care | Vaccination, Weighing | **yes** |
| Sagar Mahoor | `Sagar@2026` | operator, fallback vaccination operator | Preventive Care | Vaccination, Weighing | **yes** |
| Natheswar / Eshwar | `Natheswar@2026` | operator, CBE vaccination operator | Preventive Care | Vaccination, Weighing | **yes** |
| Chandrakant | `Chandrakant@2026` | `pc_director` | Preventive Care | Vaccination only | **no** |
| Jyothi | `Jyothi@2026` | **verifier** | Preventive Care | Verification-only backend grant | **no** |
| Pramod | `Pramod@2026` | operator | Weighing Operations | Weighing, Herd Operations (Counts) | **no** |
| Kumar Sharath | `Kumar@2026` | operator | Weighing Operations | Weighing, Herd Operations (Counts) | **no** |
| Dinakar | `Dinakar@2026` | `growth_director`; business title Growth Director | Growth | Weighing only | **no** |

- ONLY Amit + Darshan + Sagar + Natheswar count toward vaccination operator
  animal capacity.
- Chandrakant is director; Jyothi is verifier; the 4 leadership users are
  `ceo_internal`. None of them add vaccination operator capacity.
- Pramod and Kumar Sharath are CBE Weighing Operations operators. Since the
  maintainer decision of 2026-08-05 they hold Herd Operations (Counts) **on top
  of** Weighing — birth, death and shifting capture on the phone. Neither module
  was removed, and neither user gains Vaccination or vaccination capacity.
- **Chandrakant and Dinakar additionally hold `counts_approver`** (maintainer
  decision 2026-08-05): approve/reject on the birth/death/shifting queue, on the
  phone (Approvals module) and on admin-web `/approvals`. It is a PER-PERSON
  authority granted by name alongside their job role — `pc_director` and
  `growth_director` themselves carry no approval permission, so a future holder
  of either job inherits none. The named list is `perPersonGrants` in
  `backend/cmd/seed-stg-login-grants/approvers.go`; adding an email there is the
  act of granting the authority. Both grants materialize ACTIVE at seed:
  Chandrakant's Firebase UID is committed, and Dinakar's user_id is resolved from
  his existing `workforce_members` roster row (his UID is not committed to this
  repo). See `docs/runbooks/current-active-rbac-roles.md` -> "`counts_approver`
  is granted by NAME".
- **Since 2026-08-07 both also hold `operator`, and Dinakar also holds
  `pc_director`.** The `operator` grant gives them the ground surface their
  director roles omit — `counts.write` capture (birth/death/shifting), weighing
  execute, and the feed reads. Dinakar keeps `growth_director` alongside
  `pc_director`, so Weighing still has an accountable director. Neither gains
  vaccination drive capacity: the operator pool reads `workforce_positions`
  (`position_tier <> 'director'`), not the RBAC role.
- Dinakar's base job role is `growth_director`, giving both-park Growth
  visibility and Weighing scan/submit/reopen (backend permissions expose
  `weighing_execute` in `/app/bootstrap`). Superseded in part on 2026-08-07: the
  per-person grants above now also give him Vaccination (`pc_director`), Counts
  capture (`operator` -> `counts.write`) and approval authority
  (`counts_approver`). What has NOT changed is vaccination drive capacity — his
  `workforce_positions` tier is `director`, which the operator pool query
  excludes, so he still adds none.
- Counts (Herd Operations) is active for the `weighing_ops` department only
  (maintainer decision 2026-08-05). It remains `inactive` for the `health`
  department; `preventive_care` already carried it before that decision. Do not
  restate the retired blanket rule that Counts is inactive for all operator
  module grants in STG.
- **`preventive_care` no longer holds `milk` or `aas_health`** (maintainer
  decision 2026-08-05). A PC seat's bottom bar is Vaccination + Counts + Feed.
  Milk Prep / Milk Feeding and the Health worklists are gone from that bar ON
  PURPOSE — this retires the old "a department granted counts must also be
  granted milk" coupling, which existed only so the 2026-07-31 module split
  would not silently drop pages. The pages themselves are untouched
  (`/counts/milk-preparation`, `/counts/milk-feeding`, still `counts.write`),
  and the `health` department keeps both modules. Applied by migration
  `000110_preventive_care_drop_milk_aas_health.sql`; the fresh-seed half is
  `defaultDepartmentModules` in `backend/cmd/seed-roster-real/main.go`, pinned by
  `TestDefaultDepartmentModulesMatchDecisions` and
  `TestPreventiveCareDropsMilkAndHealth`.
  - This affects the four PC OPERATORS only (Amit, Darshan, Sagar, Natheswar).
    **Chandrakant keeps Health**, because `pc_director` is a leadership
    principal and `candidateModuleKeys` resolves leadership drawers from
    `leadershipModuleKeys` (`vaccination`, `weighing`, `aas_health`) and ignores
    department grants entirely. Removing Health from the director as well is a
    separate code change, not a grant change.
- Firebase allowlist alone is NOT enough and Firebase user existing is NOT enough:
  backend grant AND an active `workforce_members` profile AND `/app/bootstrap`
  context must pass — for the 4 field users AND the 4 leadership users (leadership
  profile via `ensureLeadershipMember`).
- Mobile STG access also requires Firebase App Distribution tester access.
  Whenever granting a mobile user, check/create their Firebase Auth user, backend
  grant/profile, auth allowlist, AND add them to Firebase App Distribution group
  `goatos-testers`; otherwise they can authenticate but cannot download the app.
- STG reseed sets the Firebase passwords with
  `make seed-stg-firebase-password-users`; backend grants/profiles are then
  materialized by `make seed-stg-9-person-login`.

> **STG seed is FAIL** unless Amit, Darshan, and Sagar appear as HRMS/vaccination
> operators with capacity, Chandrakant appears as director, Jyothi has verifier
> login/grant readiness, and the 4 Mesha leadership users are `ceo_internal`
> with an active profile that loads on both admin-web and the mobile app.

## STG Temporary Password Convention

For STG operator/director testing accounts, use this fixed temporary password format:

```text
<FirstName>@2026
```

Canonical current STG credentials:

| Person | Email | STG temporary password |
|---|---|---|
| Amit | amit797069@gmail.com | `Amit@2026` |
| Darshan | darshantalawar033@gmail.com | `Darshan@2026` |
| Sagar | sagarmahoor143@gmail.com | `Sagar@2026` |
| Natheswar / Eshwar | natheswar7@gmail.com | `Natheswar@2026` |
| Chandrakant | chandrakanth119527@gmail.com | `Chandrakant@2026` |
| Jyothi | jyothipvg12345@gmail.com | `Jyothi@2026` |
| Pramod | pramodsahu616285@gmail.com | `Pramod@2026` |
| Kumar Sharath | kumarsharath95279@gmail.com | `Kumar@2026` |
| Dinakar | babureddy315@gmail.com | `Dinakar@2026` |

Do not invent random passwords for these STG accounts.
Do not use a shared password.
Do not change this convention unless the maintainer explicitly asks.
If these passwords are rotated, update this runbook in the same change.

## Backend Binding Requirement

Firebase Auth password setup is not enough.
Each account must also have backend role/workforce binding so `/app/bootstrap`
returns the correct operator/director/verifier context.

A pending `auth_pending_email_grants` row is also NOT enough — it only
materializes into a real, active `user_scope_grants` row on a runtime sign-in
claim event, which admin-web SSO and mobile logins do not reliably trigger on
a fresh STG seed. Run `make seed-stg-9-person-login` (backend/cmd/
seed-stg-login-grants) every time STG is seeded: it materializes the ACTIVE
grant directly for the UID-backed canonical accounts. Leadership/director
visibility accounts may get tenant scope. Operator execution accounts get park
scope only; no operator should ever have `scope_type='tenant'` in
`user_scope_grants`. Director execution users use `pc_director` for Vaccination
or `growth_director` for Weighing, not a
tenant-scoped `operator` shortcut. The command also binds
Amit/Darshan/Sagar/Chandrakant onto their existing named `workforce_members`
roster row with `department_id = preventive_care`, so the vaccination module
renders immediately. Verify with `make verify-stg-9-person-login` and the
checklist in `docs/runbooks/stg-9-person-login-verification.md`. Do not treat
this file's credential table, on its own, as seed-complete. Jyothi's CPT
verifier row is seeded through `auth_pending_email_grants` by the CPT roster
seed; without her Firebase UID committed to `seed-stg-login-grants`, her active
verifier `user_scope_grants` row is materialized on first verified sign-in by
`/auth/session-events`.

Guardrail: every STG seed/grant change that touches field users must pass
`make stg-operator-scope-guard`. That guard blocks `operator` accounts without
an explicit park and blocks the old tenant-only operator seeding path. If a
user needs to scan in CBE, grant CBE park scope plus weighing/vaccination task
assignment; do not grant tenant scope as an operator shortcut. A director can
have both-park visibility and scan via the feature-specific director role.

## Firebase App Distribution Requirement

Mobile users must also be testers in the STG Firebase App Distribution group:

```bash
firebase appdistribution:testers:add <email> \
  --group-alias goatos-testers \
  --project goatos-stg

firebase appdistribution:testers:list --project goatos-stg
```

Treat a mobile grant as incomplete until the email appears in group
`goatos-testers`. This is separate from Firebase Auth login and separate from
the backend grant.

## Add or Fix a STG Mobile Operator Login

For a new STG mobile operator, all four surfaces must be updated. Missing any
one of these produces confusing failures: Firebase may accept the password, but
the app can still get `403 email_not_allowed`; or the backend may know the user,
but they may not be able to install the Android build.

Use this exact order:

1. Seed or rotate the Firebase Auth email/password user:

   ```bash
   gcloud config set project goatos-stg

   npm --prefix apps/admin-web run auth:seed-password-users -- \
     --project goatos-stg \
     --continue-url https://stg.dashboard.mesha.sg/login \
     --allow-project-mismatch \
     --user-password "<email>=<FirstName>@2026"
   ```

2. Add the same email to Firebase App Distribution:

   ```bash
   firebase appdistribution:testers:add <email> \
     --group-alias goatos-testers \
     --project goatos-stg

   firebase appdistribution:testers:list --project goatos-stg | rg -i '<email>|goatos-testers'
   ```

3. Create or verify the backend workforce member and active `user_scope_grants`
   row. Operator execution users need an active `workforce_members` row and a
   park-scoped `operator` grant for the park they will work in. For CBE this is:

   ```text
   tenant_id: 00000000-0000-4000-8000-000000000001
   CBE park/location id: 00000000-0000-4000-8000-000000003001
   role: operator
   scope_type: park
   scope_id: 00000000-0000-4000-8000-000000003001
   status: active
   ```

4. Add the email to the STG API auth allowlist secret and roll the API service
   so Cloud Run rereads the secret:

   ```bash
   CURRENT="$(gcloud secrets versions access latest \
     --secret=goatos-stg-auth-allowed-emails \
     --project=goatos-stg)"

   python3 - <<'PY' "$CURRENT" "<email>" > /tmp/goatos-stg-auth-allowed-emails.updated
   import sys
   current, new_email = sys.argv[1], sys.argv[2].strip().lower()
   emails = [e.strip() for e in current.split(",") if e.strip()]
   if new_email not in [e.lower() for e in emails]:
       emails.append(new_email)
   seen, out = set(), []
   for email in emails:
       key = email.lower()
       if key not in seen:
           seen.add(key)
           out.append(email)
   print(",".join(out), end="")
   PY

   gcloud secrets versions add goatos-stg-auth-allowed-emails \
     --project=goatos-stg \
     --data-file=/tmp/goatos-stg-auth-allowed-emails.updated

   REVISION="$(gcloud secrets versions list goatos-stg-auth-allowed-emails \
     --project=goatos-stg \
     --filter='state:ENABLED' \
     --sort-by='~createTime' \
     --limit=1 \
     --format='value(name)')"

   gcloud run services update goatos-api-stg \
     --project=goatos-stg \
     --region=asia-south1 \
     --update-env-vars GOATOS_AUTH_ALLOWLIST_REVISION="$REVISION"
   ```

5. Verify through the real mobile app API, not the admin dashboard. Operators
   are not admin dashboard users, so `https://stg.dashboard.mesha.sg` can still
   reject them even when mobile access is correct.

   ```bash
   API_URL="$(gcloud run services describe goatos-api-stg \
     --project=goatos-stg \
     --region=asia-south1 \
     --format='value(status.url)')"

   # Get a Firebase ID token using the Firebase web API key, then:
   curl -i "$API_URL/app/bootstrap" \
     -H "Authorization: Bearer $FIREBASE_ID_TOKEN" \
     -H "X-GoatOS-Tenant-ID: 00000000-0000-4000-8000-000000000001"
   ```

Expected success is HTTP `200` with `operator_profile`, active park-scoped grant,
and `visible_navigation` containing the modules assigned to the operator.

### Natheswar STG Login Fix - 2026-08-05

Natheswar/Eshwar (`natheswar7@gmail.com`) was added for the 2026-08-05 CBE
ET+TT vaccination drive. Firebase password auth, backend workforce row, and CBE
park operator grant were present, but login still failed because
`goatos-stg-auth-allowed-emails` did not include `natheswar7@gmail.com`. The
backend rejected the already-valid Firebase token with:

```json
{"code":"email_not_allowed","message":"this Google account is not allowed for Mesha Admin"}
```

Fix applied:

- Firebase Auth user password set to `Natheswar@2026`.
- Firebase UID verified as `iDPkAozncBPB9TcQWB1XP8qLeq02`.
- Backend stable user id verified as
  `a442ef46-034f-51af-a26b-7da27db0f6f8`.
- Workforce member verified:
  `4bd1f183-555c-4c67-b24a-4f5372e73e2a`, display name `Natheswar`,
  active CBE operator.
- Active CBE park-scoped operator grant verified:
  `09aa5c55-2866-432c-b9b6-d9bb669b5d79`.
- Added `natheswar7@gmail.com` to Secret Manager
  `goatos-stg-auth-allowed-emails` as version `9`.
- Rolled `goatos-api-stg` with
  `GOATOS_AUTH_ALLOWLIST_REVISION=9`.
- Added `natheswar7@gmail.com` to Firebase App Distribution group
  `goatos-testers`.
- Verified real STG API:
  `/app/bootstrap`, `/app/me`, and `/app/vaccination/execution` all returned
  HTTP `200` with CBE operator context.

Do not diagnose this class of failure from the admin dashboard URL alone:
operators use the mobile app API path. Admin dashboard access is separate.

# STG Operator Login Credentials

> **Canonical STG personnel rule:** `docs/runbooks/stg-login-seed-contract.md`.
> This file carries the field/verifier credential detail plus live STG weighing
> access additions. The original 10-person contract (5 SSO leadership + 4 field
> + 1 verifier) lives in the canonical runbook.

## Canonical STG Personnel Rule + Weighing Addendum

10 STG people total: **5 Mesha leadership (Google SSO **and** Firebase email/
password, `ceo_internal`, NO vaccination capacity)** + **4 field users (Firebase
email/password)** + **1 verifier (Firebase email/password, NO vaccination
capacity)**.

> **Maintainer decision 2026-07-24:** leadership is no longer SSO-only. The 5
> leadership accounts now also have Firebase email/password logins (in addition
> to Google SSO). The prior "NO password / SSO-only" leadership rule is retired.

Leadership passwords (in addition to SSO):

| Person | Firebase password | Role | Adds vaccination capacity? |
|---|---|---|---|
| Ravi | `Ravi@2026` | ceo_internal | **no** |
| Manohar (Manohark) | `Manohar@2026` | ceo_internal | **no** |
| Manju | `Manju@2026` | ceo_internal | **no** |
| Abhishek | `Abhishek@2026` | ceo_internal | **no** |
| Aryaman | `Aryaman@2026` | ceo_internal | **no** |

Field roles and vaccination capacity:

| Person | Firebase password | Role | Department | Modules visible in STG | Adds vaccination capacity? |
|---|---|---|---|---|---|
| Amit Kumar | `Amit@2026` | operator | Preventive Care | Vaccination, Weighing | **yes** |
| Darshan Talwar | `Darshan@2026` | operator, default vaccination operator | Preventive Care | Vaccination, Weighing | **yes** |
| Sagar Mahoor | `Sagar@2026` | operator, fallback vaccination operator | Preventive Care | Vaccination, Weighing | **yes** |
| Chandrakant | `Chandrakant@2026` | **director** | Preventive Care | Vaccination, Weighing | **no** |
| Jyothi | `Jyothi@2026` | **verifier** | Preventive Care | Verification-only backend grant | **no** |
| Pramod | `Pramod@2026` | operator | Weighing Operations | Weighing only | **no** |
| Kumar Sharath | `Kumar@2026` | operator | Weighing Operations | Weighing only | **no** |
| Dinakar | `Dinakar@2026` | operator app role; business title Breeding and Growth Director | Breeding & Growth | Weighing only | **no** |

- ONLY Amit + Darshan + Sagar count toward vaccination operator animal capacity.
- Chandrakant is director; Jyothi is verifier; the 5 leadership users are
  `ceo_internal`. None of them add vaccination operator capacity.
- Pramod, Kumar Sharath, and Dinakar are weighing-only STG users. Dinakar's
  business title is Breeding and Growth Director, but his current app role is
  `operator` so the deployed module gate shows only Weighing, not Vaccination
  or Counts.
- Counts is temporarily inactive for operator module grants in STG.
- Firebase allowlist alone is NOT enough and Firebase user existing is NOT enough:
  backend grant AND an active `workforce_members` profile AND `/app/bootstrap`
  context must pass — for the 4 field users AND the 5 leadership users (leadership
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
> login/grant readiness, and the 5 Mesha leadership users are `ceo_internal`
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
grant directly for the 10 UID-backed canonical accounts (this file's 4 field
users plus the 5 `ceo_internal` leadership accounts) and binds
Amit/Darshan/Sagar/Chandrakant onto their existing named `workforce_members`
roster row with `department_id = preventive_care`, so the vaccination module
renders immediately. Verify with `make verify-stg-9-person-login` and the
checklist in `docs/runbooks/stg-9-person-login-verification.md`. Do not treat
this file's credential table, on its own, as seed-complete. Jyothi's CPT
verifier row is seeded through `auth_pending_email_grants` by the CPT roster
seed; without her Firebase UID committed to `seed-stg-login-grants`, her active
verifier `user_scope_grants` row is materialized on first verified sign-in by
`/auth/session-events`.

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

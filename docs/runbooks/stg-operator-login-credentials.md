# STG Operator Login Credentials

> **Canonical STG personnel rule:** `docs/runbooks/stg-login-seed-contract.md`.
> This file only carries the field-user credential detail; the full 9-person
> contract (5 SSO leadership + 4 field) lives in the canonical runbook.

## Canonical STG Personnel Rule (9 people total)

9 STG people total: **5 Mesha leadership (Google SSO **and** Firebase email/
password, `ceo_internal`, NO vaccination capacity)** + **4 field users (Firebase
email/password)**. All use the `<FirstName>@2026` password convention.

> **Maintainer decision 2026-07-24:** leadership is no longer SSO-only. The 5
> leadership accounts now also have Firebase email/password logins (in addition
> to Google SSO). The prior "NO password / SSO-only" leadership rule is retired.

Leadership passwords (in addition to SSO):

| Person | Firebase password | Role | Adds vaccination capacity? |
|---|---|---|---|
| Ravi | `Ravi@2026` | ceo_internal | **no** |
| Manohar (Manohark) | `Manohark@2026` | ceo_internal | **no** |
| Manju | `Manju@2026` | ceo_internal | **no** |
| Abhishek | `Abhishek@2026` | ceo_internal | **no** |
| Aryaman | `Aryaman@2026` | ceo_internal | **no** |

Field roles and vaccination capacity:

| Person | Firebase password | Role | Adds vaccination capacity? |
|---|---|---|---|
| Amit Kumar | `Amit@2026` | operator | **yes** |
| Darshan Talwar | `Darshan@2026` | operator, default vaccination operator | **yes** |
| Sagar Mahoor | `Sagar@2026` | operator, fallback vaccination operator | **yes** |
| Chandrakant | `Chandra@2026` | **director** | **no** |

- ONLY Amit + Darshan + Sagar count toward vaccination operator animal capacity.
- Chandrakant is director; the 5 leadership users are `ceo_internal`. Neither
  adds vaccination operator capacity.
- Firebase allowlist alone is NOT enough and Firebase user existing is NOT enough:
  backend grant AND an active `workforce_members` profile AND `/app/bootstrap`
  context must pass — for the 4 field users AND the 5 leadership users (leadership
  profile via `ensureLeadershipMember`).
- The maintainer sets the Firebase passwords (console / Admin SDK); agents seed
  grants + profiles + docs, not login passwords.

> **STG seed is FAIL** unless Amit, Darshan, and Sagar appear as HRMS/vaccination
> operators with capacity, Chandrakant appears as director, and the 5 Mesha
> leadership users are `ceo_internal` with an active profile that loads on both
> admin-web and the mobile app.

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
| Chandrakant | chandrakant119527@gmail.com | `Chandra@2026` |

Do not invent random passwords for these STG accounts.
Do not use a shared password.
Do not change this convention unless the maintainer explicitly asks.
If these passwords are rotated, update this runbook in the same change.

## Backend Binding Requirement

Firebase Auth password setup is not enough.
Each account must also have backend role/workforce binding so `/app/bootstrap`
returns the correct operator/director context.

A pending `auth_pending_email_grants` row is also NOT enough — it only
materializes into a real, active `user_scope_grants` row on a runtime sign-in
claim event, which admin-web SSO and mobile logins do not reliably trigger on
a fresh STG seed. Run `make seed-stg-9-person-login` (backend/cmd/
seed-stg-login-grants) every time STG is seeded: it materializes the ACTIVE
grant directly for all 9 canonical accounts (this file's 4 field users plus
the 5 `ceo_internal` leadership accounts) and binds Amit/Darshan/Sagar/
Chandrakant onto their existing named `workforce_members` roster row with
`department_id = preventive_care`, so the vaccination module renders
immediately. Verify with `make verify-stg-9-person-login` and the checklist in
`docs/runbooks/stg-9-person-login-verification.md`. Do not treat this file's
credential table, on its own, as seed-complete.

# STG Operator Login Credentials

> **Canonical STG personnel rule:** `docs/runbooks/stg-login-seed-contract.md`.
> This file only carries the field-user credential detail; the full 9-person
> contract (5 SSO leadership + 4 field) lives in the canonical runbook.

## Canonical STG Personnel Rule (9 people total)

9 STG people total: **5 Mesha leadership (Google SSO only, `ceo_internal`, NO
password, NO vaccination capacity)** + **4 field users (Firebase email/password,
password `<FirstName>@2026`)**.

Field roles and vaccination capacity:

| Person | Firebase password | Role | Adds vaccination capacity? |
|---|---|---|---|
| Amit Kumar | `Amit@2026` | operator | **yes** |
| Darshan Talwar | `Darshan@2026` | operator, default vaccination operator | **yes** |
| Sagar Mahoor | `Sagar@2026` | operator, fallback vaccination operator | **yes** |
| Chandrakant | `Chandra@2026` | **director** | **no** |

- ONLY Amit + Darshan + Sagar count toward vaccination operator animal capacity.
- Chandrakant is director; the 5 SSO leadership users are `ceo_internal` SSO-only.
  Neither adds vaccination operator capacity.
- Firebase allowlist alone is NOT enough and Firebase user existing is NOT enough:
  backend grant AND `/app/bootstrap` context must pass for all 4 field users.

> **STG seed is FAIL** unless Amit, Darshan, and Sagar appear as HRMS/vaccination
> operators with capacity, Chandrakant appears as director, and the 5 Mesha
> leadership users are SSO-only `ceo_internal`.

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

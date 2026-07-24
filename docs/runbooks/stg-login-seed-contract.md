# STG Login Seed Contract

> **This is the canonical STG personnel-seed rule.** Every other STG login/seed
> runbook and `context/deploy-contract.json` point here. If any doc disagrees
> with this section, this section wins.

## Canonical STG Personnel Rule (9 people total)

There are **9 STG people total**: 5 Mesha leadership (SSO only) + 4 field users
(Firebase email/password).

### A) 5 Mesha leadership users — Google SSO only

- Log in with **Google SSO only**.
- Role/grant: **`ceo_internal`**.
- Do **NOT** create email/password credentials for them.
- Do **NOT** count them as vaccination operators.
- Seed is complete only when the SSO grant exists and `/app/bootstrap` returns
  CEO/internal context.

### B) 4 field users — Firebase email/password login

- Log in with **Firebase email/password**.
- Password convention is always **`<FirstName>@2026`**:

  | Person | First name | STG password |
  |---|---|---|
  | Amit | Amit | `Amit@2026` |
  | Darshan | Darshan | `Darshan@2026` |
  | Sagar | Sagar | `Sagar@2026` |
  | Chandrakant | Chandra | `Chandra@2026` |

- These are **STG throwaway credentials only**.
- Firebase allowed-emails is **NOT** enough.
- Firebase user existing is **NOT** enough.
- Backend grant **AND** `/app/bootstrap` context must pass.

Field roles:

| Person | Role |
|---|---|
| Amit Kumar | operator |
| Darshan Talwar | operator, **default vaccination operator** |
| Sagar Mahoor | operator, **fallback vaccination operator** |
| Chandrakant | **director** |

### Vaccination capacity rule

- **ONLY** Amit + Darshan + Sagar count toward vaccination operator animal
  capacity.
- Chandrakant is **director** and must **NOT** add vaccination operator capacity.
- The 5 SSO leadership users must **NOT** add vaccination operator capacity.

### Seed completion criteria

- all 4 field users have Firebase email/password credentials
- all 4 field users have backend grants
- all 4 field users pass `/app/bootstrap` with correct role/context
- all 5 leadership users have active `ceo_internal` SSO grants
- all 5 leadership users pass `/app/bootstrap` after SSO
- HRMS/operator screens show Amit, Darshan, Sagar as operators
- Chandrakant shows director context, not operator capacity

> **STG seed is FAIL** unless Amit, Darshan, and Sagar appear as HRMS/vaccination
> operators with capacity, Chandrakant appears as director, and the 5 Mesha
> leadership users are SSO-only `ceo_internal`.

---

STG seed must create/verify login readiness for two separate user classes.

## 1. Field Android Users — Email / Password

These users log in with Firebase email/password.

| Person | Role | Email | STG temp password |
|---|---|---|---|
| Amit | vaccination operator | amit797069@gmail.com | `Amit@2026` |
| Darshan | vaccination operator | darshantalawar033@gmail.com | `Darshan@2026` |
| Sagar | vaccination operator | sagarmahoor143@gmail.com | `Sagar@2026` |
| Chandrakant | director | chandrakant119527@gmail.com | `Chandra@2026` |

Seed requirements:

- Firebase Auth user exists in `goatos-stg`
- email/password provider enabled
- password set to the documented STG temp password unless maintainer says rotate
- backend workforce/member row exists
- backend identity/role binding exists
- `/app/bootstrap` returns the correct operator/director context

Agents must not invent random passwords.
Agents must not use shared passwords.
Agents must not create leadership password users.

## 2. Leadership Users — SSO Only

The 5 CEO/internal leadership users log in through SSO.

Seed requirements:

- no email/password login is created for leadership users
- SSO identity is allowlisted for STG
- `ceo_internal` grant exists
- backend identity/role binding exists after claim/login
- `/admin-web/bootstrap` returns leadership / CEO internal context

Agents must not invent passwords for leadership users.
Agents must not reset leadership SSO passwords.
Agents must not confuse leadership SSO users with field Android users.

## Required Seed Verification

After STG seed, report:

| user class | email | auth method | Firebase/SSO exists | backend grant | bootstrap context | PASS/FAIL |
|---|---|---|---|---|---|---|

STG login seed is incomplete unless both classes are verified.

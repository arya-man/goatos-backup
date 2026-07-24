# STG Login Seed Contract

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

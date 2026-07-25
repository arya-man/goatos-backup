# STG Login Seed Contract

> **This is the canonical STG personnel-seed rule.** Every other STG login/seed
> runbook and `context/deploy-contract.json` point here. If any doc disagrees
> with this section, this section wins.

## PENDING GRANTS ARE NOT ENOUGH (mandatory, every STG seed)

`auth_pending_email_grants` rows (created by `make seed-stg-email-grants`) are
**not** a login grant. They only materialize into an active
`user_scope_grants` row via the `/auth/session-events` claim path, which fires
on a runtime sign-in. Admin-web leadership (Google SSO) and mobile operators/
director do **not** reliably hit that path on the first login attempt after a
fresh STG seed — the observed failure mode is `403 permission_denied` on
admin-web and an empty bottom bar on mobile.

A STG seed is **INCOMPLETE** until:

1. all 9 UID-backed accounts have an **ACTIVE** (`status = 'active'`)
   `user_scope_grants` row — not merely a pending email grant — and Jyothi has
   an active CPT verifier pending-email grant that materializes on first
   verified sign-in;
2. **all 9 UID-backed accounts have an active `workforce_members` profile.** The mobile
   `/app/bootstrap` (`activeProfileAndGrants`) hard-requires a profile row for
   the signed-in user and returns `403 operator_profile_missing` without one —
   this applies to the 5 leadership users too, not just field users. The 4
   field users bind to their existing named roster row (see step 3); the 5
   leadership users get an `auth:<uid>` profile via `ensureLeadershipMember`.
   Admin-web tolerates a missing profile; the phone does not. (Historical
   failure mode: leadership could open admin-web but got "Couldn't load your
   workspace" on the Android app because only field users were given a
   profile.)
3. the 4 field users (Amit, Darshan, Sagar, Chandrakant) are bound to their
   existing named `workforce_members` roster row with a non-null
   `department_id` (`preventive_care`), so `department_module_grants` gives
   them the vaccination bottom bar; and
4. `docs/runbooks/stg-9-person-login-verification.md` has been run for the
   UID-backed accounts, plus the Jyothi verifier sign-in/grant check passes.

`make seed-stg-firebase-password-users` is the required STG credential step. It
sets the documented Firebase email/password credential for all 10 STG people
against `goatos-stg`, fails if any Firebase user is missing, and fails if any
leadership account is not already linked to the `google.com` provider. Backend
grant materialization depends on the committed Firebase UID table, so this step
must not create replacement Firebase users during seed.

`make seed-stg-9-person-login` (backend/cmd/seed-stg-login-grants) is the
permanent, idempotent command that materializes step 1 and 2 directly for the
original 9 UID-backed accounts — it does not wait for a claim event. Both
commands are wired as required final steps of `make seed-vaccination-source-full`
and `make seed-vaccination-cpt-operator-drive` when `GOATOS_ENV=stg`, in this
order: Firebase passwords first, DB grant/profile materialization second, and
`make seed-stg-postflight` third. Postflight verifies login grants plus the
cloud-facing wiring that must be true after seed: GCS proof storage, FCM
notification config, and CEO AI/chatbot secrets/runtime env. Run
`make verify-stg-9-person-login` afterward for the human bootstrap checklist.
Jyothi is an additional CPT verifier account without a committed Firebase UID;
her verifier grant is seeded by the CPT roster into `auth_pending_email_grants`
and becomes active on first verified sign-in through `/auth/session-events`. Do
not consider a STG seed done on pending-grant output alone unless that exception
is explicitly the Jyothi UID-less verifier lane and her sign-in check has been
run.

## Canonical STG Personnel Rule (10 people total)

There are **10 STG people total**: 5 Mesha leadership (Google SSO and Firebase
email/password) + 4 field users (Firebase email/password) + 1 proof verifier
(Firebase email/password).

### A) 5 Mesha leadership users — Google SSO **and** email/password

> **Maintainer decision 2026-07-24:** leadership is no longer SSO-only. The
> previous "Google SSO only / do NOT create email/password credentials" rule is
> **RETIRED**. Leadership now logs in with **either** Google SSO **or** Firebase
> email/password, same as field users.

- Log in with **Google SSO OR Firebase email/password** (either works).
- Role/grant: **`ceo_internal`**, tenant scope.
- Password convention is the same `<FirstName>@2026` used for field users:

  | Person | STG password |
  |---|---|
  | Ravi | `Ravi@2026` |
  | Manohar (Manohark) | `Manohark@2026` |
  | Manju | `Manju@2026` |
  | Abhishek | `Abhishek@2026` |
  | Aryaman | `Aryaman@2026` |

- These are **STG throwaway credentials only**. The STG seed runs
  `make seed-stg-firebase-password-users`, which sets the Firebase
  email/password credential for each existing leadership account in the
  `goatos-stg` Firebase project. The SSO (`google.com`) provider stays linked.
- Do **NOT** count them as vaccination operators.
- Seed is complete only when the `ceo_internal` grant is ACTIVE, an active
  `workforce_members` profile exists (see mandatory section above), and
  `/app/bootstrap` returns CEO/internal leadership context.

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

### C) 1 verifier — Firebase email/password login

- Jyothi logs in with **Firebase email/password**.
- Email: `jyothipvg12345@gmail.com`.
- Password: `Jyothi@2025`.
- Role/grant: **`verifier`**, tenant scope.
- Seed path: the CPT operator-drive roster seeds an active
  `auth_pending_email_grants` row for this email/role during DB seed. Because
  the repo does not commit Jyothi's Firebase UID, the active
  `user_scope_grants` row is materialized by the normal `/auth/session-events`
  claim path on first verified sign-in.
- Jyothi is proof-review only and must **NOT** add vaccination operator capacity.

### Seed completion criteria

- all 4 field users have Firebase email/password credentials
- all 4 field users have backend grants
- all 4 field users pass `/app/bootstrap` with correct role/context
- all 5 leadership users have active `ceo_internal` grants
- all 5 leadership users have Firebase email/password credentials (in addition
  to Google SSO) and an active `workforce_members` profile
- all 5 leadership users pass `/app/bootstrap` (mobile) AND
  `/admin-web/bootstrap` after either SSO or password login
- Jyothi has Firebase email/password credentials, a verifier pending email
  grant from CPT DB seed, and `/admin-web/bootstrap` verifier access after first
  sign-in materializes the active grant
- HRMS/operator screens show Amit, Darshan, Sagar as operators
- Chandrakant shows director context, not operator capacity
- Jyothi never appears as a vaccination operator and never contributes to
  operator animal capacity

> **STG seed is FAIL** unless Amit, Darshan, and Sagar appear as HRMS/vaccination
> operators with capacity, Chandrakant appears as director, and the 5 Mesha
> leadership users are `ceo_internal` with both Google SSO and Firebase
> email/password login available. Jyothi must have verifier login/grant
> readiness. Leadership and verifier users still add no vaccination capacity.

---

STG seed must create/verify login readiness for two separate user classes.

## 1. Field Android Users — Email / Password

These users log in with Firebase email/password.

| Person | Role | Email | STG temp password |
|---|---|---|---|
| Amit | vaccination operator | amit797069@gmail.com | `Amit@2026` |
| Darshan | vaccination operator | darshantalawar033@gmail.com | `Darshan@2026` |
| Sagar | vaccination operator | sagarmahoor143@gmail.com | `Sagar@2026` |
| Chandrakant | director | chandrakanth119527@gmail.com | `Chandra@2026` |

Seed requirements:

- Firebase Auth user exists in `goatos-stg`
- email/password provider enabled
- password set to the documented STG temp password unless maintainer says rotate
- backend workforce/member row exists
- backend identity/role binding exists
- `/app/bootstrap` returns the correct operator/director context

Agents must not invent random passwords (use the `<FirstName>@2026` convention).
Agents must not use shared passwords.

## 2. Leadership Users — SSO **and** email/password

The 5 CEO/internal leadership users log in through **either** Google SSO **or**
Firebase email/password (maintainer decision 2026-07-24; the prior SSO-only rule
is retired).

Seed requirements:

- Firebase email/password provider is added to each leadership account in
  `goatos-stg` (SSO `google.com` provider stays as well)
- password follows the `<FirstName>@2026` convention (`Ravi@2026`,
  `Manohark@2026`, `Manju@2026`, `Abhishek@2026`, `Aryaman@2026`)
- SSO identity is allowlisted for STG
- `ceo_internal` grant exists and is ACTIVE
- an active `workforce_members` profile exists (auto-created by
  `ensureLeadershipMember` in `seed-stg-login-grants`) — required for the
  mobile `/app/bootstrap`
- `/admin-web/bootstrap` returns leadership / CEO internal context AND the
  mobile `/app/bootstrap` returns leadership context

Agents must not invent random passwords (use `<FirstName>@2026`).
Agents must not confuse leadership users with field Android users for the
vaccination-operator capacity rules (leadership never add operator capacity).

> Credential-setting boundary: STG reseed sets the documented throwaway
> passwords through `make seed-stg-firebase-password-users`. Production
> credentials and any password rotations still require maintainer approval.

## Required Seed Verification

After STG seed, report:

| user class | email | auth method | Firebase/SSO exists | backend grant MATERIALIZED (active, not pending) | department bound (field users) | bootstrap context | PASS/FAIL |
|---|---|---|---|---|---|---|---|

STG login seed is incomplete unless both classes are verified, the grant
column reads ACTIVE (never "pending only"), and the full checklist in
`docs/runbooks/stg-9-person-login-verification.md` has been executed.

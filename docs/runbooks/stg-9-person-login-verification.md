# STG 9-Person Login Verification

> Canonical personnel rule: `docs/runbooks/stg-login-seed-contract.md`.
> Credential detail: `docs/runbooks/stg-operator-login-credentials.md`.
> This file is the post-seed verification checklist for both. Run it after
> EVERY STG seed (`make seed-stg-9-person-login`, or a seed chain that wires
> it in) — a green `make seed-stg-9-person-login` run is necessary but not
> sufficient; this checklist is the sufficiency proof.

## Why this exists

`auth_pending_email_grants` rows are NOT a login grant — they only become a
real `user_scope_grants` row via the `/auth/session-events` runtime claim
path, which fires on sign-in. Admin-web (Google SSO) and mobile
(operator/director) logins do not reliably trigger that path on the FIRST
attempt after a fresh STG seed, so the observed failure is a 403
`permission_denied` on admin-web or an empty bottom bar on mobile — even
though `seed-stg-email-grants` reported success. `make seed-stg-9-person-login`
closes this by writing the active grant directly. This checklist proves it
actually happened, for all 9 accounts, every time.

**A STG seed is INCOMPLETE until every row in every table below is checked.**

## 1. Grant materialization (SQL — required, not optional)

Run against the STG database (via Cloud SQL Auth Proxy, per
`docs/runbooks/google-cloud-environments.md`):

```sql
-- Every one of the 9 accounts must show status = 'active' here.
-- A row that only exists in auth_pending_email_grants (not here) is a FAIL.
SELECT role, status, count(*) AS n
FROM user_scope_grants
WHERE tenant_id = '00000000-0000-4000-8000-000000000001'
  AND scope_type = 'tenant'
GROUP BY role, status
ORDER BY role, status;
-- Expect at least: ceo_internal/active >= 5, operator/active >= 5,
-- pc_director/active >= 1, growth_director/active >= 1, verifier/active >= 1
```

```sql
-- Cross-check: no account should be pending-only. This returns EMPTY on pass.
SELECT p.email, p.role, p.status AS pending_status
FROM auth_pending_email_grants p
WHERE p.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND p.status = 'active'
  AND NOT EXISTS (
    SELECT 1 FROM user_scope_grants g
    WHERE g.tenant_id = p.tenant_id
      AND g.role = p.role
      AND g.scope_type = 'tenant'
      AND g.status = 'active'
  );
```

`make verify-stg-9-person-login` runs a quick version of the first query.

## 2. Department binding (field users only — SQL)

```sql
-- Amit, Darshan, Sagar, Chandrakant must each show a non-null department_id
-- pointing at the preventive_care department. This returns EMPTY on pass.
SELECT wm.display_name, wm.user_id, wm.department_id
FROM workforce_members wm
WHERE wm.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND wm.status = 'active'
  AND wm.display_name IN ('Amit Kumar', 'Darshan Talwar', 'Sagar Mahoor', 'Chandrakant')
  AND wm.department_id IS NULL;
```

```sql
-- Confirm the department carries the vaccination module.
SELECT d.code, dmg.module_key, dmg.status
FROM departments d
JOIN department_module_grants dmg ON dmg.department_id = d.department_id
WHERE d.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND d.code = 'preventive_care';
-- Expect a row with module_key = 'vaccination', status = 'active'.
```

## 2b. Workforce profile presence (ALL 9 — SQL, required)

The mobile `/app/bootstrap` (`activeProfileAndGrants`) returns
`403 operator_profile_missing` for any signed-in user with no active
`workforce_members` profile — **including the 5 leadership users.** Field users
get a named roster row (Section 2); leadership get an `auth:<uid>` profile from
`ensureLeadershipMember`. This query must return **9**:

```sql
SELECT count(*) AS active_profiles
FROM auth_pending_email_grants g
JOIN workforce_members wm
  ON wm.user_id = g.last_claimed_user_id
 AND wm.status = 'active'
WHERE g.tenant_id = '00000000-0000-4000-8000-000000000001'
  AND g.normalized_email IN (
    'ravi@mesha.sg','manohark@mesha.sg','manju@mesha.sg','abhishek@mesha.sg','aryaman@mesha.sg',
    'amit797069@gmail.com','darshantalawar033@gmail.com','sagarmahoor143@gmail.com','chandrakanth119527@gmail.com'
  );
-- Expect: active_profiles = 9. Anything < 9 is a FAIL: the missing account(s)
-- authenticate but get "Couldn't load your workspace" on the Android app.
```

Historical failure (2026-07-24): all 5 leadership users had an active
`ceo_internal` grant but NO `workforce_members` row, so SSO worked yet the
Android app showed "Couldn't load your workspace." `ensureLeadershipMember`
now closes this in `seed-stg-login-grants`.

## 3. Live bootstrap/app checks (manual, per account)

| # | Person | Email | Surface | Expected |
|---|---|---|---|---|
| 1 | Ravi | ravi@mesha.sg | admin-web `/admin-web/bootstrap` | `ceo_internal`, every built module visible |
| 2 | Manohar K | manohark@mesha.sg | admin-web `/admin-web/bootstrap` | `ceo_internal`, every built module visible |
| 3 | Manju | manju@mesha.sg | admin-web `/admin-web/bootstrap` | `ceo_internal`, every built module visible |
| 4 | Abhishek | abhishek@mesha.sg | admin-web `/admin-web/bootstrap` | `ceo_internal`, every built module visible |
| 5 | Aryaman | aryaman@mesha.sg | admin-web `/admin-web/bootstrap` | `ceo_internal`, every built module visible |
| 6 | Amit Kumar | amit797069@gmail.com | mobile `/app/bootstrap` | `operator`, vaccination module in bottom bar |
| 7 | Darshan Talwar | darshantalawar033@gmail.com | mobile `/app/bootstrap` | `operator` (default vaccination operator), vaccination in bottom bar |
| 8 | Sagar Mahoor | sagarmahoor143@gmail.com | mobile `/app/bootstrap` | `operator` (fallback vaccination operator), vaccination in bottom bar |
| 9 | Chandrakant | chandrakanth119527@gmail.com | mobile `/app/bootstrap` | `pc_director`, director/monitoring context, NO vaccination operator capacity |

Report this table filled in (PASS/FAIL per row) before declaring a STG seed
done. A row that 403s or shows an empty bottom bar is a FAIL — file it as a
login-materialization defect, not a "wait for next sign-in" note.

> Leadership (rows 1–5) now also log in on the **mobile** app (SSO or
> `<FirstName>@2026` password) and must load the leadership mobile context
> (Calendar / Overview / Alerts) via `/app/bootstrap` — not just admin-web.
> A leadership account that opens admin-web but 403s on the phone is a FAIL
> (Section 2b catches the usual cause: missing `workforce_members` profile).

## 4. STANDING invariants this checklist enforces

- Pending grants are NOT enough — see `docs/runbooks/stg-login-seed-contract.md`.
- Only Amit + Darshan + Sagar count toward vaccination operator animal
  capacity. Chandrakant and the 5 leadership accounts must NOT.
- Do not invent random passwords or use a shared password — use the
  `<FirstName>@2026` convention. Leadership now DO get email/password logins in
  addition to SSO (maintainer decision 2026-07-24); see
  `docs/runbooks/stg-operator-login-credentials.md`. STG reseed sets the
  documented Firebase passwords through `make seed-stg-firebase-password-users`.
- ALL 9 accounts (not just field users) must have an active
  `workforce_members` profile — Section 2b. Leadership profiles are the
  `auth:<uid>` rows created by `ensureLeadershipMember`; do not fabricate a
  named roster row for them.
- Do not fabricate a `workforce_members` roster row to pass department
  binding — Section 2 must match an EXISTING named roster row seeded by
  `seed-roster-real` / `seed-vaccination-cpt-operator-drive`. A missing/
  ambiguous match is a `seed-stg-login-grants` FAIL, not something to route
  around with a synthetic roster insert.

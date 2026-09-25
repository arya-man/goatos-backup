# Mesha Exchange — System Design (v1: own LPs)

Owner: Ravi · Date: 25 Sep 2026 · Status: draft for build
Scope: Manju (CEO) sets up pools and records LP investments; each LP logs in and sees only their own holding, value and portfolio split. No blockchain, no trading, no legal workflow in v1.

---

## 1. Decisions

| Question | Decision |
|---|---|
| Units or tokens? | **Units.** A unit is an equal share of one pool. No tokens. |
| Unit price | Units start at **1.00 of the pool's base currency** (₹1, $1…, set in pool config). Every later investment is **forward priced**: it buys units at the first approved valuation on or after the day the money arrives ("awaiting units" until then), so nobody buys at a stale price and earlier LPs are never diluted. |
| Global LPs | Money arrives in any allowed currency, is converted to the pool's base currency at the rate on the receipt date, then buys units. Each LP sees values in their chosen display currency, always alongside base currency and units. |
| Pool shape | A pool is scoped to **one or more parks** (optionally narrowed to farms/sheds). Manju creates pools on top of the existing park/farm/shed setup. |
| How an LP is split across pools | Manju records one investment **per pool** (e.g. ₹20 L → ₹12 L Pool A + ₹8 L Pool B). The LP's portfolio = sum of their pool holdings. |
| What an LP owns | Units in a pool → a % of everything in that pool. Never title to a specific goat or acre. |
| Where it lives | A **separate repo (`vgoats/mesha-exchange`), GCP project, service and database**, not new tables in the goatos DB. |
| Apps | Web: Next.js + MUI Minimal (Standard Plus licence) with the Mesha theme at **exchange.mesha.sg**. iOS/Android: the same web app wrapped with **Capacitor**, over-the-air updates for the web layer. API in Go. |
| Who can see money data | Manju (owner), the LP themselves, and **only people Manju grants an Investments level to from the HRMS People → Access screen**. No goatos role — admin, CEO_internal, devs, directors — gets access by default, and nobody but the owner can grant it. |
| Configuration | Everything that is a business choice (currencies, unit start price, FX source, valuation rules, approval mode, splits, who has access) is **config Manju sets in the admin UI**, not code. |

---

## 2. Architecture

```
                    ┌──────────────── goatos (existing) ────────────────┐
 Staff apps ───────▶│ goatos-api  →  goatos Postgres (parks, farms,      │
                    │               sheds, animals, weighings, sales)    │
                    └───────────────┬────────────────────────────────────┘
                                    │ read-only, NON-financial facts only
                                    │ (herd counts, live kg, realised ₹/kg,
                                    │  park/farm/shed ids+names) via a
                                    │  narrow internal API / view
                                    ▼
 Investor web ─┐        ┌─────────────────────────────┐      ┌──────────────┐
 Investor app ─┼──TLS──▶│ exchange-api (Cloud Run,    │─────▶│ exchange DB  │
 Manju admin  ─┘        │ own service account)        │      │ (own Cloud   │
                        │  • authz + RLS session vars │      │  SQL instance│
                        │  • envelope crypto via KMS  │      │  CMEK, RLS)  │
                        └──────────┬──────────────────┘      └──────────────┘
                                   │ encrypt/decrypt only
                                   ▼
                          Cloud KMS key ring `exchange`
                          (decrypt granted ONLY to exchange-api SA)
```

**Why separate:** goatos has many roles, BigQuery exports, analytics, the Ask Mesha / CEO AI agent (`ceo_ai.*` views) and dev access to the goatos DB. Putting LP money in that DB means every one of those paths becomes a leak path. A separate instance with its own IAM removes them by construction.

Data flows **one way**: exchange-api reads operational facts from goatos. Goatos never reads exchange data.

---

## 3. Data model (exchange DB)

Money is stored as **integer minor units** (paise, cents) with an explicit currency code (`bigint` + `char(3)`). Units as `numeric(20,6)` (6 decimals, never rounded when summed). Everything has `created_at`, `created_by`, `row_version`.

### Setup (Manju)
- **`pool`** — id, name, status (`draft|open|closed`), base_currency, unit_price_initial (default 1.00 in base currency, minor units), accepted_currencies[], opened_on, nav_approval_mode (`manual|auto_within_limits`), auto_approve_max_move_pct.
- **`pool_scope`** — pool_id, scope_type (`park|farm|shed`), goatos_location_id, share_pct (default 100). Lets one park be split across two pools (e.g. 60/40) or one pool span several parks.
- **`pool_asset`** — pool_id, class (`land|infra|industry|working_capital`), name, goatos_location_id (nullable), share_pct, notes. Current value lives in `asset_valuation`.
- **`asset_valuation`** — pool_asset_id, value_paise, as_of, basis, entered_by. Append-only; the latest one per asset is used.
- **`livestock_rule`** — pool_id, price_per_kg_paise, cost_to_sell_pct, price_source, effective_from. Livestock value is computed, never typed.

### People
- **`investor`** — id, type (`individual|entity`), display_currency, country, display_name_enc, legal_name_enc, email_enc, phone_enc, pan_enc, bank_enc, email_bidx (blind index for lookup), status (`invited|active|suspended|exited`).
- **`investor_user`** — investor_id, auth_uid (Firebase), role (`owner|viewer`). One LP entity can have several logins (e.g. family office + accountant).

### Money (append-only ledger)
- **`ledger_entry`** — id, pool_id, investor_id, kind (`subscription|payout|redemption|transfer_in|transfer_out|correction`), units_delta, cash_base_minor, paid_currency, paid_amount_minor, fx_rate, fx_source, fx_as_of, nav_per_unit_used, effective_date, reference, note_enc, reverses_entry_id, prev_hash, row_hash.
  - Never updated or deleted. A mistake = a `correction` row that reverses it.
  - `prev_hash`/`row_hash` chain makes any edit made directly in the DB detectable.
- **Holdings are derived:** `units(investor, pool) = Σ units_delta`. Stored in a materialised `holding` view for speed; the view is rebuilt from the ledger, never written by hand.

### Valuation
- **`nav_run`** — id, pool_id, as_of, status (`computed|approved|superseded`), inputs_json (herd head, live kg, ₹/kg, asset values used), computed_by (job), approved_by.
- **`nav_snapshot`** — nav_run_id, class, value_paise; plus pool totals: gross, liabilities, nav, total_units, nav_per_unit.

### Security
- **`access_log`** — who (auth_uid, role), what (investor_id/pool_id, action: `view|export|edit`), when, ip, user_agent, prev_hash, row_hash. Append-only.
- **`crypto_key_version`** — tracks which KMS key version wrapped each DEK (for rotation).

---

## 4. The maths (all server-side)

```
livestock_value(pool) = Σ over animals in pool scope:
                        live_kg × price_per_kg × (1 − cost_to_sell) × scope share_pct
pool_gross            = livestock + Σ latest asset_valuation × share_pct
pool_nav              = pool_gross − liabilities
nav_per_unit          = pool_nav / total_units(pool)

investor_share(pool)  = units(investor, pool) / total_units(pool)
investor_value(pool)  = investor_share × pool_nav
investor_split(class) = investor_share × class value            ← the donut
portfolio             = Σ over pools
gain                  = portfolio value + Σ payouts − Σ invested
```

**Subscription (forward pricing):** `base_cash` = INR actually credited (reference `fx_rate` on the receipt date kept for display). The entry is recorded as **awaiting units** and excluded from NAV per unit. When the **first approved valuation dated on or after the receipt date** is published, `units = base_cash / nav_per_unit(that valuation)`. A pool with no units yet opens at `unit_price_initial` (1.00); existing assets get sponsor units first. Never price new money off an earlier (stale) NAV.

**Display:** `value_in_display = investor_value(base) × fx(base→display, today)`, always shown with base amount, units, rate, rate date and source. Foreign LPs carry INR/FX risk; the screen makes it visible.

---

## 5. Security design

### 5.1 Layers (each one alone should be enough to stop a leak)

1. **Network / IAM** — separate Cloud SQL instance, private IP only, no public IP, no Cloud SQL Proxy grants for humans. Only the `exchange-api` service account can connect. Devs and goatos admins have **no IAM role** on this project/instance.
2. **Application authz** — every request resolves `auth_uid → (grant level, pool scope)` or `(investor, investor_id)` from **exchange-owned grants** (sections 11–12). Goatos roles (`RoleCEOInternal`, admin, directors) map to **nothing** here; an HRMS grant only counts if it exists in the exchange DB.
3. **Postgres RLS** — `ENABLE` + `FORCE ROW LEVEL SECURITY` on every table with investor data. The app connects as `exchange_app` (not owner, no `BYPASSRLS`). Per transaction:
   ```sql
   SET LOCAL app.role = 'investor';            -- or 'staff'
   SET LOCAL app.investor_id = '<uuid>';       -- investors
   SET LOCAL app.pool_ids = '{<uuid>,...}';    -- staff: pools their grant covers
   -- policy
   CREATE POLICY row_access ON ledger_entry
     USING ( (current_setting('app.role') = 'investor'
              AND investor_id = current_setting('app.investor_id')::uuid)
          OR (current_setting('app.role') = 'staff'
              AND pool_id = ANY (current_setting('app.pool_ids')::uuid[])) );
   ```
   If the app forgets to set the variables, `current_setting` errors → the query fails closed.
4. **Field encryption (envelope, Cloud KMS)** — PII (names, email, phone, PAN, bank, notes) is encrypted in the app with a per-investor data key (DEK, AES-256-GCM). DEKs are wrapped by a KMS key that **only the exchange-api SA can use**. A DB dump, backup or a DBA with SQL access sees ciphertext only.
   - Lookups by email use a **blind index** (HMAC-SHA256 with a KMS-protected key), not the plaintext.
   - Money fields (paise, units) stay plaintext **inside this isolated DB** so the NAV and split maths can run. Protecting them is the job of layers 1–3; field encryption on amounts would break aggregation for little gain once the DB itself is sealed.
5. **Audit** — every staff read of an investor's data writes `access_log`. LPs can see "who viewed my portfolio" in their profile. Manju's views are logged too.

### 5.2 Passwords and login
- We **never store passwords**. Login uses Firebase Auth (already used by goatos) in a **separate Firebase tenant / project** for investors, so staff accounts and LP accounts never mix.
- MFA mandatory for Manju and all LPs (TOTP or passkey). Magic-link invite for first login; LP sets their own credential.
- Sessions: 15-minute idle timeout on web, re-auth for exports and for any edit Manju makes.
- Android: biometric unlock, `FLAG_SECURE` (no screenshots/recents preview), no on-disk cache of portfolio data.

### 5.3 "Even devs and admins can't read it"
- No human has DB credentials in normal operation. Migrations run from CI with a separate `exchange_migrator` role that owns schema but is not used by the app.
- **Break-glass** only: a time-boxed IAM grant (Privileged Access Manager / JIT), approved by Ravi **and** Manju, auto-expires in 1 hour, fully audit-logged. Even then the dev sees ciphertext for PII, because KMS decrypt stays with the service account.
- Staging and local use **synthetic data only**. Production backups can never be restored into a non-prod project (org policy + separate CMEK key).

### 5.4 Leak paths to close explicitly
| Path | Rule |
|---|---|
| Logs (Cloud Logging, Sentry) | Structured logs carry ids only; a redaction middleware drops request/response bodies on exchange routes. |
| BigQuery / analytics exports | Exchange DB is **not** connected to any export. `investor-web-shadow` must stop reading BigQuery for this. |
| Ask Mesha / CEO AI agent / data map | Exchange data is **never** added to `ceo_ai.*` views or the agent's tools. |
| LangSmith / LLM tracing | Nothing from exchange-api goes to LLM tooling. |
| Frontend analytics | No third-party analytics or session replay on investor pages. Strict CSP. |
| Statement PDFs / exports | Generated on demand, short-lived signed URLs (5 min), watermarked with LP name + time. |
| Emails / notifications | Say "Your statement is ready", never amounts. |
| Admin-web screenshots / smoke tests | Visual smoke uses synthetic data; production screens are excluded from screenshot bots. |

---

## 6. Manju's admin flow

1. **Create pool** → pick parks (existing locations), optionally narrow to farms/sheds and set share %.
2. **Add pool assets** → land, infra, industry stakes, working capital; enter value + basis.
3. **Set livestock rule** → ₹/kg, cost to sell %.
4. **Add LP** → name, contact, PAN/bank (optional), type; send invite.
5. **Record investment** → LP, amount, date, pool (or split across pools). The system shows the units it will issue and the NAV/unit used before saving.
6. **Record payouts** → per pool: total cash; the system splits by units held on that date and previews each LP's amount.
7. **Approve NAV** → the nightly job computes; Manju approves (or it auto-approves if within set limits). LPs only ever see approved NAV.

---

## 7. LP view

Only approved numbers, only their own data:
- Overview: invested, current value, gain, NAV/unit chart per pool.
- Split: donut by asset class across all their pools; per-pool breakdown.
- Drill-down: pool → park → farm → shed → animal (herd facts are not sensitive; their ₹ share is).
- Transactions: their ledger entries only.
- Profile: logins on their account, "who viewed my data" log.

---

## 8. Edge cases

**Pools and scope**
- Park/shed split across pools → `pool_scope.share_pct`; the sum per location must be ≤ 100% (checked on save).
- Animals move between sheds/parks (movement module) → value follows scope at NAV time; history uses the scope as of that date.
- Park deactivated or renamed in goatos → pool keeps its `location_id`; the name is shown as of now, with a warning if the location is inactive.
- Pool with assets but zero units → Mesha must first take **sponsor units** for those assets (in-kind), so the first LP doesn't get them for free. An empty pool opens at the configured start price (1.00).
- Closing a pool → no new subscriptions; final payout, then units go to zero via redemption entries.

**Money**
- Backdated investment (entered today, dated last month) → uses the approved NAV of that date; later NAVs are recomputed and the change is flagged to Manju.
- No approved NAV on that date → blocked until one exists (or use the last approved one before it, shown explicitly).
- Correcting a wrong entry → reversal + new entry; LP sees both, with a note.
- Rounding → units to 6 decimals, paise integers; payout remainders stay in the pool (never create or lose a paisa).
- Payout split → by units held at the payout's **record date**, not today.
- Partial exit / redemption / transfer between LPs → ledger kinds exist from day one even if the UI comes later.
- Negative or zero NAV (mass mortality) → allowed, clearly shown; no divide-by-zero.
- Two edits at once (Manju on two devices) → `row_version` optimistic locking; the second save is rejected.
- Currency → INR stored; SGD shown as a translation with rate and time.

**People and access**
- LP is a company or family → `investor.type = entity`, several `investor_user` logins, each with their own MFA.
- LP loses phone / MFA → recovery only via Manju + identity check; logged.
- LP leaves → status `exited`; access to statements kept, data retained per policy, logins disabled.
- Manju unavailable → one named backup `exchange_owner`, added only with Ravi + Manju approval.
- Staff member also an LP → they log in to the investor app as an LP; their goatos role grants nothing extra.
- Invite sent to the wrong email → invite links are single-use, expire in 48 h, and bind on first login with MFA.

**Security**
- App bug forgets the RLS session variables → queries fail closed (tested).
- SQL injection → sqlc typed queries only; RLS still limits blast radius.
- Insider with goatos admin → no path: different DB, different IAM, different auth tenant.
- Stolen backup → CMEK-encrypted; PII additionally encrypted with a separate KMS key.
- KMS key rotation → new DEKs wrapped with the new version; old ones re-wrapped lazily.

---

## 9. Tests that must exist (CI gate)

- RLS: investor A can never read investor B (every table, every endpoint), including through `holding` and `nav` views.
- Fail-closed: a query with no session variables errors.
- Goatos admin / CEO_internal / dev tokens get 403 on every exchange route.
- Ledger: sum of units = total units per pool; no UPDATE/DELETE possible on `ledger_entry` (the DB role lacks the grant); hash chain verifies.
- Maths: fixtures for subscription at NAV, payout split, rounding, backdating.
- Leak checks: no PII in logs (log scraper test), no exchange tables in any BigQuery or CEO AI config.
- p95 < 500 ms on LP overview and Manju's LP list.

---

## 10. Build order

| # | What | Rough time |
|---|---|---|
| 1 | GCP setup: exchange project/instance, KMS key ring, service account, Firebase investor tenant, CI migrator | 3–4 days |
| 2 | Schema + RLS + crypto layer + RLS/fail-closed tests | 1 week |
| 3 | Goatos read API for herd facts per location (non-financial) | 3 days |
| 4 | NAV job + approval | 1 week |
| 5 | Manju admin screens (config, pools, assets, LPs, investments, payouts) + HRMS Investments access row | 1.5–2 weeks |
| 6 | Investor web (prototype design) | 1.5 weeks |
| 6b | Shared feature-flag store with targeting (replaces static appconfig map) | 3–4 days |
| 7 | Android investor screens | later, same API |

About 5–6 weeks to Manju loading real LPs and LPs seeing their portfolio.

---

## 11. Configuration and access (Manju-controlled)

### 11.1 Config Manju sets in the admin UI
| Area | Settings |
|---|---|
| Exchange-wide | Accepted currencies; FX rate source (e.g. RBI reference / a fixed provider) and fetch time; default display currency; rounding (unit decimals); statement branding. |
| Per pool | Name, parks/farms/sheds and share %; base currency; unit start price (default 1.00); accepted currencies; open/closed for new money; NAV approval mode and auto-approve limit. |
| Valuation | Livestock ₹/kg source and value, cost to sell %, which weighings count (max age); asset values and basis per land/infra/industry item. |
| Payouts | Record date rule, which currency payouts go out in (LP's paid currency or base), rounding remainder rule. |
| LPs | Profile, type, display currency, logins, invite/suspend/exit. |
| Access | Who on staff can see or do what (11.2). |

Every config change is versioned (who, when, before → after) and needs step-up MFA. Changes that affect money (price rule, FX source, share %) apply **from a stated effective date**, never retroactively, unless Manju explicitly runs a recompute that is logged.

### 11.2 Staff access through HRMS
The existing HRMS **People → Access** editor (workforce `AccessService` + permissions `ModuleCapability` catalog, levels View / Do / Configure / Oversee) gets a new **Investments** row, so Manju grants it the same way as any other module:

| Level (goatos order) | Can |
|---|---|
| View | See LP list, holdings, values, splits, ledger (read-only) for the pools in scope. PII masked unless the grant also has `pii`. |
| Do | View + record investments and payouts, add/invite LPs (drafts if maker-checker is on). |
| Oversee | Do + approve NAV runs and drafts made by others. |
| Configure | Oversee + pool setup, valuation rules, currencies, asset values, **grant/revoke Investments access**. Held by Manju (and named backup). |

Optional scope per grant: all pools or selected pools. Grants can have an expiry date (e.g. auditor for 2 weeks).

**The safety rule:** the Investments row is shown in HRMS, but the **grant is written to and enforced by exchange-api**, not stored in goatos `user_scope_grants`.
- Only a user who already holds Investments → Configure can change that row; for everyone else (including goatos admins and CEO_internal) it is read-only or hidden.
- Saving calls exchange-api with the granter's investor-tenant session + step-up MFA; exchange-api records the grant in its own `staff_grant` table and `access_log`.
- So someone who can edit HRMS in goatos (or edit the goatos DB) still cannot give themselves access.
- Revoking in HRMS, or the person leaving (HRMS status → exited), revokes immediately via an event exchange-api subscribes to; access also stops at grant expiry.
- Staff with a grant still log in with MFA; everything they view is in the LP-visible "who viewed my data" log.

New table: **`staff_grant`** — person_id (HRMS), auth_uid, level, pool_ids (null = all), valid_from, valid_to, granted_by, revoked_at, reason.

### 11.3 Extra edge cases from config + access
- Manju changes base currency of a pool that already has units → blocked; base currency is fixed once the first unit is issued.
- FX source missing a rate for a date → use the last rate before it, flagged on the entry; never silently use today's rate.
- Money received in a currency the pool doesn't accept → cannot be saved until Manju adds it to accepted currencies.
- Grantee is also an LP → their staff grant and LP login are separate sessions; as LP they see only themselves.
- Last Configure holder removed → blocked; there must always be at least two Configure holders (Manju + backup).
- Grant expired mid-session → next request is refused; UI returns to login.

## 12. Permission model (GitHub / Meta Business style)

### 12.1 What we borrow
| Product | Pattern | How it lands here |
|---|---|---|
| GitHub | Org → teams → repos; built-in roles (read/triage/write/maintain/admin) + custom roles; outside collaborators; required 2FA; org audit log; expiring fine-grained tokens | Exchange → pools; built-in levels View/Do/Oversee/Configure + custom roles later; **external grants** (auditor, CA) with mandatory expiry; MFA required for every grant; audit log LPs can see |
| Meta Business Manager | People + assets (Pages, ad accounts); per-asset **partial vs full** access; minimum two admins; partner businesses; Security Center | Grants are per pool (the asset), with capability toggles like `pii` and `export`; minimum two Configure holders; partner = external firm grant |
| Both | Deny by default; secrets visible to nobody (GitHub secrets are write-only even to admins) | Bank/PAN fields are **write-only** after entry: shown masked (••••4410) to everyone, including Manju, unless a separate `pii.reveal` action with step-up MFA is used and logged |

### 12.2 Model
```
Principal  = HRMS person | LP user | external person | team (group of people)
Resource   = exchange → pool → investor → data class (financials | pii | documents)
Role       = named bundle of actions (built-in: view/do/oversee/configure; custom later)
Grant      = (principal, role, resource scope, extra capabilities[], valid_from, valid_to,
              conditions: {mfa_required, ip_allowlist?}, granted_by, reason)
Decision   = can(principal, action, resource) → allow | deny  (deny wins, default deny)
```
- **Teams:** Manju can make a team such as "Finance" and grant it View on Pool A; adding someone in HRMS to the team gives them access. Removing them, or the person exiting, removes it.
- **Actions** are fine-grained strings, e.g. `investor.read`, `investor.pii.reveal`, `ledger.write`, `nav.approve`, `pool.configure`, `access.grant`, `export.statement`. Built-in levels are just bundles of these, the same way goatos `ModuleCapability.Levels` maps a level to permissions.
- **One decision function** in exchange-api (`can(...)`). The API calls it on every request, and the same grants produce the RLS session variables, so the API and the database can never disagree.
- **"View as LP"** for Manju: a read-only preview of exactly what an LP sees, logged, and shown in that LP's "who viewed my data" list.
- **Access review:** every quarter, Manju gets a list of all grants and must re-confirm or remove each one. Grants not re-confirmed expire.
- **Personal access tokens** (for a future accounting export or integration): scoped to one pool and read-only, expiring in 90 days or less, shown once, stored hashed.

### 12.3 Feature flags (separate from permissions)
Permissions decide **who** may see data; flags decide **whether a feature is switched on**. They are never used as a security control.

Goatos today has a static mobile flag map (`appconfig.defaultFeatureFlags`). The exchange needs a flag store with targeting:
- **Flag** = key, description, owner, default, kill-switch flag, expiry/cleanup date.
- **Targeting rules** in order: user → team/role → tenant → % rollout → default.
- **Every change audited**, with instant rollback; flags are cached with a short TTL so a kill switch takes effect in under a minute.
- **Example flags:** `exchange.enabled` (whole module), `exchange.android_investor`, `exchange.multi_currency_display`, `exchange.lp_access_log`, `exchange.exit_requests` (off in v1).
- Build it once as a shared goatos service, replacing the static map, so both goatos and the exchange use it.

### 12.4 Tests
- Every action × every built-in level × in-scope/out-of-scope pool → expected allow/deny (table-driven, like the existing `permissions/*_test.go`).
- API and RLS agree for every grant fixture.
- Expired, revoked and exited-person grants deny on the next request.
- PII stays masked for View/Do/Oversee/Configure unless `pii.reveal` is used, and every reveal is logged.
- Turning a flag off never exposes data; turning one on never grants access.


## 13. Hardening

### 13.1 Pricing and money
- **Forward pricing.** Money buys units at the first approved valuation on or after the day it arrives; until then the ledger entry is "awaiting units" and is excluded from NAV per unit. Buy-backs price the same way and need enough pool cash. Stops anyone buying or selling at a stale price.
- **Maximum NAV age.** No dealing if the latest approved valuation is older than 31 days, or while a newer draft is waiting.
- **Dealing suspended** when NAV per unit is zero or below.
- **Sponsor units** for assets a pool already holds before the first LP (see §8).
- **Closed periods.** After a valuation is approved or a payout record date passes, a backdated entry needs an Oversee-approved restatement run; it can't just be recorded.
- **Corrections** keep the original's effective date and trigger a restatement of the affected valuations. Payouts are reversed as a whole batch, never one LP's row.
- **FX.** The INR actually credited by the bank is the source of truth; the reference rate (stored per day in `fx_rate`) is kept for display. v1 pools are INR-based only; LPs still pay and view in their own currency. Payouts go out in INR unless an LP's payout currency is set, converted at the payment-date rate.
- **Rounding.** Integer paise and `numeric` in Go (no floats). Payout remainders are written as their own ledger row so the books balance.
- **Animals moving between pools.** A goat moving from a park in Pool A to Pool B is a priced transfer event in the exchange at live value, and `pool_scope` changes are versioned by date. The approval screen shows head-count and kg changes from goatos, and auto-approve is blocked if either jumps more than a set %.

### 13.2 Nobody alone can read or move the data
- **"No single person"**, not "no dev": exchange-api holds the KMS decrypt right, so a malicious deploy could leak data. Controls:
  - CODEOWNERS on `migrations/`, `authz/` and `crypto/`, approved by someone other than the author and named by Manju;
  - Binary Authorization: only CI-built images from `main` at a signed SHA;
  - OTA bundles signed with keys outside the dev team and verified by the native shell;
  - egress from exchange-api allowed only to KMS, Cloud SQL, the goatos read API and the FX provider.
- **Grants live only in the exchange.** Investments access is granted in exchange admin by a Configure holder signed in to the exchange's own staff login with MFA. HRMS only deep-links to that page and can **revoke** (someone leaves), never add. Teams are exchange-owned too. So editing HRMS or the goatos DB can't create access.
- **Levels follow goatos:** View; Do = View + record; Oversee = View + approve (not a superset of Do); Configure = everything. Maker-checker is **mandatory** for valuations, reversals, payouts, buy-backs and grants. At least two Configure holders without an expiry date.

### 13.3 Database
- RLS uses `current_setting('app.x', true)` inside a `SECURITY DEFINER` helper that raises when the setting is missing or empty, so pooled connections can't fall through. Explicit `app.all_pools` flag for all-pool staff; a branch for Configure; policies on `investor`, `investor_user` and `access_log` too.
- No materialised views over RLS tables. `holding` is a normal table maintained by trigger with its own RLS, or a `security_invoker` view. Tested through the real connection pool.
- **Tamper evidence that works:** one hash chain per pool, written under an advisory lock. The daily head hash is copied to a locked-retention bucket in a separate GCP project and printed on each LP's statement. Same for `access_log`.

### 13.4 Auth and apps
- Identity Platform in the exchange GCP project with two tenants, **LPs** and **exchange staff**. MFA by TOTP; passkeys via WebAuthn built by us. LP recovery needs two staff, a waiting period and a notice to every login on the account. Invites are bound to the email's blind index.
- **Web:** `Cache-Control: no-store` on every authenticated response, no SSR/CDN caching of LP pages, strict CSP.
- **Apps (Capacitor):**
  - Android `FLAG_SECURE`; on iOS, blur the screen when the app goes to the background and detect screen recording;
  - no API data kept in WebView storage; session held in Keychain/Keystore.
- **Apple review:** native biometric lock, push and Keychain give the app real native features (guideline 4.2). OTA is used for web-layer fixes only; any native change goes through review (2.5.2).
- **Feature flags** run in the exchange's own store (code can be shared with goatos), targeting opaque cohort ids, so goatos admins never see LP names in flag rules or hold a kill switch.

### 13.5 Performance and logging
- Unwrapped data keys are cached in memory with a short TTL, so Manju's LP list doesn't make one KMS call per LP (keeps it under 500 ms). Emails are normalised before blind-indexing, and there is a re-index plan for key rotation.
- "Who viewed my data" also lists break-glass sessions and "View as LP" previews. Nightly jobs log under a system principal. Sentry uses `beforeSend` scrubbing.
- **Legal (parked by decision):** pooled units with offshore LPs may be regulated. Record the decision to skip legal in v1, and keep LP-facing words to "units" and "value", not "returns".

## 14. Open questions for Ravi / Manju

1. One pool per park to start, or pools spanning several parks?
2. Should Manju be able to see all LPs' amounts in one table? (Assumed yes.)
3. Who should be the backup Configure holder besides Manju?
4. Should NAV auto-approve daily, or does Manju approve each one?
5. Confirm the investor domain `exchange.mesha.sg`.

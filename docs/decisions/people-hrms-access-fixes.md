# People / HRMS access fixes (2026-10-02)

A real STG case: a sales hire was added on People / HRMS to get **only** Sales > Vendors (buyers) and the
market survey. It took several direct database edits, because of seven gaps. Each is fixed at the
root, with a failing test first. This page is the canonical prose; the older decision docs carry a
dated pointer here.

## 1. The Add Person role list is the designation catalog

The role dropdown was a literal list in the page contract, and the write path a closed map of
fifteen roles in Go (`grantablePersonRoles`). Sales Director, Sales Manager, Procurement Director /
Manager and HR — all `designation_catalog` rows — could not be chosen.

- `people_roles` is compiled from **active** `designation_catalog` rows whose code is a role the
  form may grant (`permissions.GrantableFromAddPerson`): never `ceo_internal` (the founder/builder
  cohort is seed-owned), never the retired `operator`.
- The create path checks the **same catalog**, in the preflight (before any Firebase account) and in
  the write transaction (`ports.ErrRoleNotOffered` → 400 `invalid_role`).
- Scope comes from `permissions.RoleWorksAcrossEveryPark`, now the **one** answer that
  `internal/parkscope` also reads. Its own list had drifted: Breeding Director was offered at tenant
  scope but refused by the park-scope writer ("a person with only park roles must be limited to
  parks"), so creating one failed on `origin/main`. HR and every composite `director_*` key are
  farm-wide.
- The person's access header records the designation they started as.
- Migration `000464` adds the `director_sales` / `manager_sales` designation rows (already on STG by
  hand).

## 2–3. Screens offered from the ticks on screen; one save

The editor offered a module's screens from what had been **saved**, so a module switched on for the
first time showed no screen chips, and its save was refused "needs at least one screen ticked". The
workaround was two saves, phone first.

The editor payload now carries **every** screen with its `required_permissions` and each module's
`web_level_permissions`. The modal (`features/people/access-pages.ts`) works out the openable
screens from the draft ticks, so the chips on screen are exactly what Save sends.

## 5. One rule: web ticks open web screens

The editor and the save counted **phone** ticks when deciding which web screens were openable; the
sidebar resolver loaded **web** rows only. A screen could be offered, ticked and saved, then never
appear.

**Decision: a phone tick never opens a web screen.** `PageAccessForAssignments` and the save's
`validatedAssignments` both read `permissions.WebPermissionsForAssignments`. Live sidebars do not
change (the resolver already read web rows only); the editor stops offering what it then hid. Route
authorization still reads the person's whole permission set — a route cannot tell which surface
called it. This supersedes the "permissions union across surfaces, so a person holding Feed at
configure on the phone can open Feed Config on the web" sentence in
`per-person-page-access.md`, which the sidebar never actually honoured.

## 4. The vendor register's two halves are granted separately

One permission (`procurement.vendor.read`) opened both halves of the register, and the list
endpoint trusted the `side` the client asked for. A buyers-only person needed the Vendors module,
which showed — and served through the API — every supplier.

- New permissions `procurement.vendor.sales.read` / `.write` (`VendorSalesRead` / `VendorSalesWrite`)
  for the **buyers**; `VendorRead` / `VendorWrite` now mean the **suppliers**. `VendorFinanceRead`
  stays shared.
- New capability module **`vendors_sales`** ("Vendors · buyers"); `vendors` is "Vendors · suppliers".
  Sales > Vendors is ticked on `vendors_sales` and opened on `VendorSalesRead`; it still sits in the
  Sales sidebar group.
- The vendor routes admit **either** half; the vendor handler attaches the caller's side access
  (per-person permissions, role fallback) and the service enforces it on **every** read and write:
  a named side the caller lacks is 403 `vendor_side_forbidden`; no side means the whole register only
  for someone holding both, otherwise their one half; a vendor on the other half is **not found**;
  a create / update / status change onto a half they cannot write is refused. A vendor's side is its
  record type's catalog `register_side`; uncatalogued is procurement (the complementary rule).
- Buyer phone numbers (Buyer analytics) follow `VendorSalesRead`.
- The vendor finance check now reads the per-person permission set too (it read role grants only).
- Migration `000465` writes `vendors_sales` exactly where a person could reach the buyers before —
  web: Vendors + Sales with Sales > Vendors ticked (or all pages); phone: Sales + Vendors anywhere —
  removes the stale `sales-vendors` page tick from Sales rows, retires a web Sales row narrowed to
  that one page, and does the same for designation pre-fills. Ledgered; the Down path is exact.
  Role maps (the role-path fallback) carry both halves wherever they carried `VendorRead`.

## 6. The phone menu is the person's ticks, whatever their role

A "leadership" role (Feed / Growth / PC / Health / Breeding Director, Park Head) capped the phone:
`leadershipModuleKeys` was intersected with the ticks, so ticks could only remove. A sales hire (Feed
Director + buyers + market ticks) saw only Clock.

For a person with stored rows the phone candidates are now `renderableModuleKeys(ticks) + Clock` for
every principal. Nothing widens beyond the ticks; each tab still needs its own permission; the
standalone-verifier exemption and the 2026-09-25 "no My Work on a director's phone" exclusion stay.
A person with no stored rows keeps the role path. This supersedes "leadership module offers are NOT
reachable from the database … giving a director a module is always a code change"
(`docs/agent-rules/procurement.md`): ticking it on People / HRMS gives it.

## 7. The phone's Sales tabs are given one at a time

The Sales phone module's three tabs answer to three separate ticks, enforced by the backend:
**Ledger** ← Sales (`SalesRead`), **Vendors** ← Vendors · buyers (`VendorSalesRead`), **Market** ←
Market survey (`MarketEntry`). `vendors_sales` and `market_survey` both render into the Sales
module, so either brings it onto the phone; the ledger tab and its routes need `SalesRead`, so they
stay hidden and refused without the Sales tick.

## 8. The Farm value "Over 35 kg" card follows the page, not a role

The card read the whole Weights report (`/weighing/shed-weights`, WeighingMonitor) and its control
was gated on a ROLE carrying that permission, so a person given Farm value on People / HRMS saw it
disabled. Maintainer: "he should see that value whether he has weighing page access or not".

- Two narrow reads, each open to **WeighingMonitor or SalesRead** and carrying only what the card
  shows: `GET /growth-director/sale-ready-line` (the two sale-ready figures) and
  `GET /weighing/sale-ready-count` (the count). No pen, tag, weight or price leaves through them;
  `/weighing/shed-weights` and the full assumptions stay on Weighing.
- The card's control is enabled for every reader of the page.
- Same root as bug 6, fixed where it lived: the Growth Director module checked ROLE grants only
  (`RolesAuthorize(actor.Roles, …)`), and weighing's park scope (`authorizedParkSet`) read role
  grants only. Both now read the person's resolved permissions and park scope first, exactly as
  the route middleware does, so a Weighing tick on People / HRMS opens the Weights reads instead of
  "not found".

## The sales hire, end to end, with no database edits

Add Person → **Sales Director** (now offered, tenant scope) → Access: **Vendors · buyers** on web
and phone (Sales > Vendors ticked) + **Market survey** on the phone → one save. Web sidebar: Sales >
Vendors only. Phone: Sales with Vendors + Market tabs, plus Clock. Procurement > Vendors is visible
nowhere, and `GET /procurement/vendors?side=procurement` (or a supplier's id) is refused.

## Tests

`TestCreatePersonAcceptsTheBreedingDirectorRoleHintWithDockerPostgres` (red on main),
`TestCreatePersonGrantsASalesDirectorFromTheDesignationCatalogWithDockerPostgres`,
`TestAddPersonGrantsEveryDesignationThatIsARole`, `TestEveryFarmWideRoleIsTenantOnlyInParkScope`
(mutation-tested), `TestAddPersonRolesComeFromTheDesignationCatalog`,
`TestAModuleSwitchedOnForTheFirstTimeOffersItsScreensAndSavesInOneGo`,
`TestEditorOffersExactlyTheScreensTheSidebarShows` (mutation-tested), `access-pages.test.mjs`,
`TestABuyersOnlyCallerNeverReachesASupplier`, `TestASuppliersOnlyCallerCannotAddABuyer`,
`TestBuyersModuleBackfillKeepsEveryoneOnTheScreensTheyHad` (real Postgres, Up and Down),
`TestThePhoneMenuIsThePersonsTicksWhateverTheirRole` (mutation-tested),
`TestDirectorsStillGetNoPhoneWorkBoardFromTicks`, `TestFarmValueReadersGetTheOver35CountAndNothingElse`
and `TestWeighingTickedOnPeopleOpensTheWeightsReport` (mutation-tested: ignoring the person scope
reproduces "not found"), `TestTheSaleReadyLineFollowsTheTicksAndCarriesNothingElse`.

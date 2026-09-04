# Page-grain access retires the admin-web role lenses

Maintainer decision, 2026-08-27. Supersedes the MECHANISM of the 2026-08-21
procurement-director workspace decision; its OUTCOME is preserved byte for byte.

## What was wrong

Two different layers decided what a person saw on admin-web, and they disagreed.

1. **Permissions.** What the person is allowed to do.
2. **Lenses.** Hand-written Go that deleted nav leaves and page contracts for a
   named ROLE, regardless of permissions.

The Procurement Director holds `feed_config.read/write`, `operators.*`,
`roster.*` and `verification.act` through the `feed_director` role he also
wears. His sidebar showed none of it, because
`adminui/app/procurement_director_lens.go` kept only the Procurement and Feed
groups and explicitly hid `/feed/config`.

Both layers were correct about their own question. Together they meant a screen
reading the permission layer — the new People access editor — would show modules
the person could not reach, and every future "this person should not see that
page" was a code change.

## The decision

**A person's admin-web sidebar is exactly the pages ticked for them.** One layer.

- The module tick says whether they touch the module at all.
- Page ticks inside it say which of its screens they open.
- `procurement_director_lens.go` is DELETED. Its narrowing was written onto the
  holder's own rows by the backfill, once, as data.

**The verifier lens STAYS** (maintainer instruction, same day). It is not the
same kind of thing: it does not subtract from the ordinary console, it composes
a DIFFERENT workspace — a queue, its own registry-built modules, its own
landing. Retiring it would delete a product surface, not a narrowing.

**The phone does not change.** Android composes its bar from the mobile module
registry; a page tick is web-only and never reaches it.

## What the model is

`permissions/capability_pages.go` holds the catalog: every admin-web nav leaf,
its module, its sidebar label, its route. Three properties:

1. **Pages are web-only.**
2. **An empty page list means EVERY page of that module.** This is what makes a
   page shipped tomorrow reach whoever already holds the module, rather than
   silently reaching nobody until someone re-ticks thirty people. Narrowing is
   opt-in.
3. **The catalog is asserted against the real navigation.**
   `TestEveryNavLeafIsATickablePage` fails if a leaf ships without a row here
   (it would be unwithholdable) and `TestEveryPageContractRouteIsOwnedByAModule`
   fails if a page contract's route belongs to no module (it could never be
   narrowed).

Storage is `person_module_access.pages` (migration `000220`). Resolution is
`permissions.PageAccessForAssignments`, read by BOTH the bootstrap narrowing and
the access editor, so the ticks the editor shows are the ticks the sidebar obeys.

## Two fail-open cases, deliberately

A person with **no stored rows** is not narrowed at all — they are still on the
retired role path, and narrowing them to nothing would lock out anyone the
backfill has not reached. A **source error** is likewise not a narrowing: it is
logged and the full contract is served. The sidebar is a convenience; the route
behind every page is independently permission-gated, and the 403 is the lockout.

## A screen is offered only when it can be OPENED

A module tick is coarser than a screen, and two layers used to decide separately whether a
leaf appeared: the tick put it there, and a second role-based RBAC pass
(`adminui/app.permissionsForNav`) greyed it. When they disagreed the person got a row that
rendered and could never be opened -- the exact "you should not even see it" case this model
exists to remove.

So every page declares the permissions its own screen needs, and a screen whose permissions
the person does not hold is never ticked. `permissionsForNav` and the catalog are asserted
identical by `TestPageCatalogPermissionsMatchTheNavigationGate`, so they cannot drift again,
and `TestNoLeafCanRenderGreyedForAPageTickedPrincipal` asserts the property end to end.

**Openability is a property of the WHOLE permission set, not of the owning module.** Feed SOP
is grouped under Feed and needs `sop.read`, which lives in Protocols & SOPs. Checking only the
owning module hid it from the CEO, who plainly holds that permission. The module is where a
screen is TICKED; it is not where its authority comes from.

Note that permissions union across SURFACES, so a person holding Feed at `configure` on the
phone can open Feed Config on the web even with only `view` ticked there. That is consistent
-- a route does not know which surface called it -- and it means removing a module from web
removes the SCREENS, not the underlying ability, while the phone still grants it.

## Nine dead screens this uncovered

An exhaustive live sweep of all 31 people (`tools/dev/audit-person-access.py`, PASS 3) opened
the DATA ROUTE behind every visible leaf and failed on a 403. It found nine leaves that
rendered and then failed for four real people, all from the same pre-existing cause: the
navigation gate had NO entry for them, so they showed for anyone whose sidebar carried the
group.

| Leaf | Was gated on | Its data route needs |
| --- | --- | --- |
| Herd Analytics, Milk Preparation | nothing | `counts.read` |
| Herd Operations SOP, Milk SOP, Feed SOP, Weighing SOP | nothing | `sop.read` |
| Feed Config, Feed Analytics | nothing | `feed_config.read`, `feed_direction.read` |
| Live Drive Tracker, Live Monitor | nothing | vaccination reads, `herd_signals.read` |
| Counts Breakdown | `goat.read` | `counts.read` |

Counts Breakdown is the one that was gated and gated WRONGLY, which is worth separating: it
had an entry, the entry named a different permission from its own route, and three people saw
a leaf that 403'd. Each gate is now the permission that leaf's own route already requires.

## Two refusals on the write path

- A page key belonging to another module, or to no module, is REJECTED. A
  dropped tick reads on screen as granted while granting nothing.
- A granted module that HAS screens with NONE ticked is REJECTED. It would
  resolve to every page (property 2), which is the opposite of what the admin
  just did on screen.

## Proof

`TestRetiredProcurementDirectorLensIsReproducedByTicks` composes the holder's
backfilled ticks — both roles stacked, as he really holds them — and asserts the
sidebar is exactly the six leaves his live bootstrap served on 2026-08-27:
Source Entry, Vendors, Sales, Feed Purchases, Feed Analytics, Feed SOP; that
`/feed/config`, `/people`, `/verify` and `/` carry no page contract; and that
the kept leaves still do. `TestCeoIsNeverNarrowed` pins the leadership exemption
both retired lenses carried.

`tools/dev/audit-person-access.py` is the live sweep: 930 checks across all 31 people and
both surfaces -- bootstrap integrity, module absence, grant/revoke per role-shape on a single
token, the dead-screen sweep, reflection timing and the refusal matrix. It is not a unit test
and not in CI (it needs the local stack and a seeded roster); run it after a change to the
access model, before landing.

Verified live on the local stack the same day: his bootstrap before and after
the retirement is identical, ticking Feed Config in the editor made the leaf and
its page contract appear with no code change, and unticking it removed them.

## One source for "which park" (maintainer decision 2026-09-04)

Three records answered "which park does this person work in", and nothing kept them in
step: the scope on each `user_scope_grants` row, the People screen's
`person_access.scope_mode` + `person_park_scope` ticks, and
`workforce_members.primary_location_id`. Vaccination assignment trusted the home park
(migration `000223`), weighing trusted grants, the capability resolver trusted the ticks,
and calendar / approvals / the blind resolver read grants again. On 2026-09-01 a
Channapatna operator claimed Coimbatore milk work: a Coimbatore grant had been added to
him by hand, his ticks and home park still said Channapatna, and no reader agreed with
another about him.

**The People screen ticks are the ONLY authored park scope.** Everything else is derived
from them, in the same transaction, by `backend/internal/parkscope`:

- `scope_mode = 'tenant'` → every role the person holds gets ONE tenant-scoped grant row;
  park rows for those roles are revoked.
- `scope_mode = 'parks'` → every role gets one row PER TICKED PARK; tenant rows and rows
  for unticked parks are revoked.
- **Home park** is a field on the editor (`home_park_id`). With one ticked park it IS that
  park; with more than one the admin must choose, because guessing would move someone's
  vaccination drives to a park they only cover occasionally. It is written to
  `workforce_members.primary_location_id`, which the vaccination trigger keeps reading
  unchanged.

Every writer of a grant row goes through it: the editor (`SavePersonAccess`), person
creation (`CreatePerson`), the operators grant API (`CreateGrant` adds a ROLE and no longer
honours the body's park once ticks exist; a person never set up is set up from the
request), the login-time email claim (`ReconcileUser` after the claim, so a tenant-scoped
pending grant cannot widen a narrowed person), and `seed-stg-login-grants`. Roles remain
their own axis and are not decided here; scopes other than tenant/park (shed, cohort,
custodian) are not park membership and are left alone.

Pinned by `TestGrantScopeHasOneWriter` (fails on any new `INSERT INTO user_scope_grants`
outside the recorded writers), `TestSavePersonAccessDerivesGrantsAndHomeParkFromTheTicks`,
`TestCreateGrantLandsTheRoleOnTheAuthoredScopeNotTheBody`,
`TestCreateGrantOnAnUnsetPersonAuthorsTheScope`,
`TestReconcileUserPullsAStrayTenantRowBackOntoTheTicks`,
`TestSavePersonAccessRefusesParksModeForATenantOnlyRole`,
`TestCreateGrantRefusesATenantOnlyRoleOnAParksPerson`, `TestEmailClaimLandsOnTheAuthoredScope`,
`TestCreatePersonAuthorsTheScopeAndDerivesTheGrant`, `TestReconcileUserLeavesATenantOnlyRoleAlone`
and `TestSavePersonAccessResolvesTheHomePark`. Proven in Chrome on an isolated stack on
2026-09-04: widening an operator to two parks showed the Home park select and derived the
second grant; clearing the home park, narrowing the verifier to a park, and setting an
operator to Every park were each refused on screen with the backend's own sentence and
left the rows unchanged. The derivation was mutation-tested while being
written: re-reading the role list after the revoke statement left a tenant-mode person
holding no roles, and the integration test caught it.

**Tenant-only roles cannot be narrowed.** `parkscope.TenantOnlyRoles` (CEO, verifier, every
director, `counts_approver`, `toxin_tester`) work across every park by definition. A parks-mode
save or a grant-API call that would put one of them on a park is REFUSED with the role named,
never applied: narrowing would either lock the person out (their routes drop park-scoped
grants) or show a director half the herd. The login-time claim cannot refuse a login, so it
leaves such a row as written and the next People-screen save is where the admin decides.

**Park roles need a park.** The mirror image: a person holding ONLY park roles (operator,
park head, procurement manager) is refused "Every park" (`parkscope.ErrParkRolesNeedAPark`).
This is the standing operator-scope invariant made enforceable: no real operator receives
tenant scope, and a park head covering both parks is two ticks, never tenant mode. Tenant
mode is for someone who also carries a tenant-only role (Chandrakant, Dinakar).

**Concurrency.** Every derivation locks the person's `workforce_members` row, so the editor
and the grant API cannot both pass the not-exists check and leave a duplicate active row.

**Drift check.** `tools/dev/park-scope-drift.sql` is a read-only query that lists every
person whose grant rows, ticks or home park disagree; it must print nothing on STG after
the 2026-09-04 repair, and is the first thing to run when someone reports seeing the other
park.

Known boundary: readers that still consult grants directly (weighing operator offer,
calendar scope, approvals) are correct by construction now that grants equal ticks, but
they are not yet routed through the person scope. That is a follow-up, not a second
source.

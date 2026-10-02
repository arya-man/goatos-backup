# Testing: what "test it" means (Claude, Codex, every agent)

When the maintainer says **"test it"**, **"check it"**, **"verify"** or **"make sure it works"**, it
means the checks below, not "the unit tests pass". This file exists because the People / HRMS case
of 2026-10-02 shipped seven gaps that every existing test passed over
(`docs/decisions/people-hrms-access-fixes.md`): a person needed several database edits to get one
screen. Each section names the gap it would have caught.

Write the result of every section in the handoff, including the ones that did not apply and why.
A section silently skipped counts as not tested.

## 1. Test as a brand-new user, not only as an existing one

Run the flow for someone who has **nothing saved yet**, then again for someone who already has data.
Most fixtures and seeded people already have rows, which hides first-time bugs.

*Missed:* a module switched on for the first time showed no screens to tick, and its save was
refused. It only worked after a first save, so every test using an already-set-up person passed.

## 2. One action must be enough

If the only way to finish is a second save, a different order, a refresh or a database edit, that
is a bug, not a workaround. Do the user's action once and assert it worked.

*Missed:* "save the phone column first, then the web one" was the only way through.

## 3. Lists on screen come from the data, not from code

For every dropdown, picker or list of options, compare what the screen offers with the table it
should come from (`designation_catalog`, `procurement_vendor_catalog`, `shed_partitions`, ...).
Add a row to that table in a throwaway database and confirm the screen offers it with no deploy.

*Missed:* the Add Person role list was a constant, so Sales Director, Sales Manager, Procurement
Director and HR could not be chosen.

## 4. Every surface must agree

The same fact must give the same answer in every place that shows it: the editor that grants it,
the web sidebar, the phone menu, and the API. Check all of them for the same person, not just the
one you changed.

*Missed:* the editor counted phone ticks and the sidebar did not, so a screen could be ticked,
saved and never appear.

## 5. Test more than one role, and the roles that branch

Run the flow as at least one person from each kind of role the code treats differently: CEO/CXO,
a director in the "leadership" list (Feed, Growth, PC, Health, Breeding Director, Park Head), a
director outside it (Sales, Procurement), a ground manager, and a verifier. Look for
`isLeadershipPrincipal`, role maps and `switch role` branches first; each branch is a case to
test.

*Missed:* a Feed Director ticked for Sales saw only Clock on the phone, because the leadership
branch ignored the ticks. A Sales Director, outside that branch, worked fine.

## 6. Try what the user must NOT be able to do, against the API

Hiding a button is not access control. For each thing the person should not have, call the API
directly as that person:

- the other half of a split resource, by query parameter (`?side=procurement`) and with no
  parameter at all;
- a record they should not see, **by its id**;
- a write onto something they should not change.

Expect 403 or "not found" for each. A 200 is the bug.

*Missed:* "buyers only" was a screen setting, while the API served every supplier to anyone who
could read buyers.

## 7. Check the UI at both widths, and look inside scroll boxes

The responsive rule in `docs/agent-rules/ui-frontend.md` applies (1440 and 390, read the
screenshots). Also:

- **Measure the inner boxes, not just the page.** The page body can be 390 wide while a modal or
  table inside it is 540 wide and clipped. Compare `scrollWidth` with `clientWidth` on the modal,
  its body and the table.
- **Read every column header.** Narrow columns overlap each other's words.
- **Make sure every control is reachable without panning sideways.** A tick hidden off-screen is
  a tick nobody can use on a phone.
- **Look for global CSS that overrides a page.** For example `.main table{min-width:540px}` at
  phone width beats a single-class page rule.

*Missed:* the Notifications matrix headers overlapped at laptop width, and the access editor's
ticks sat off-screen at phone width.

## 8. Data migrations: real-shaped data, both directions

Seed the people and rows that exist in production (CEO, a narrowed person, someone with nothing),
run the Up, assert nobody gained or lost a screen, run the Down, and assert it is exact. Then run
it on a throwaway clone of the OCI database and read the counts.

## 9. Prove the bug before the fix

Write a test that fails on the old code for the exact case reported, then fix. For a guard, break
the code on purpose and confirm the test goes red (mutation check). Report both.

## 10. Test harness traps that look like product bugs

These made a correct build look broken during the 2026-10-02 run. Rule them out before
"fixing" the product:

- **A click lands before React hydrates on a `next dev` page.** It does nothing, especially right
  after a code edit triggers a recompile. Wait for the result, retry the open, and fail loudly
  if it never happens.
- **A selector matches a hidden twin.** For example a loading shell with the same class. Select
  by a label that only the real element carries.
- **In zsh, a variable named `path` overwrites `PATH`.** Every command after it is "not found".
- **The `pgtest` template build takes longer than a test's 90-second context over the tunnel.**
  Build it once with a longer timeout, or use the in-memory test Postgres.
- **The shared OCI database has schema drift from other branches.** Fix it in your throwaway clone
  only, never in `goatos`.

## The People / HRMS run, as a worked checklist

For any change to people, roles, access, navigation or permissions, the run that covers 1–9 is:

1. **Add Person:** add someone with a role that was recently added to `designation_catalog`, one
   save.
2. **Access editor:** for that person, switch a module on, check the screen chips appear, then
   save once.
3. **As that person (mint a token):** check the web sidebar (`/admin-web/bootstrap`), the phone
   menu (`/app/bootstrap`), and that each forbidden API call is refused.
4. **Regression the other way:** repeat step 3 as the CEO and as a director, and confirm nothing
   they had was lost.
5. **Screenshots:** capture the editor and the People tabs at 1440 and 390, and measure the
   inner widths.

The scripted version of this run is described in the PR that added this file.

# Simple tasks get their own phone tab, defined on the web

Maintainer instruction, 2026-10-01 (chat): *"if I add one more task in future ... I should not
code ... I should just define it in web, add an icon there, and design what filters it will have,
whether it has pens or pens with partitions, and what they upload -- everything SOP driven."*
Narrowed in the same conversation: *"only simple things ... like fumigation we added in preventive
care -- just two videos upload is required -- those we need to configure. Feed wastage, direction,
packing we can't configure."* Then: *"okay implement this"*.

Status: ACCEPTED, built on `feat/simple-tasks`.

## What is configurable, and what is not

A **simple task** is work that only needs: a pen (every pen, chosen pens, or the whole park), a
schedule, one person who does it, questions and photo/video captures, and optionally a verifier.
The server computes nothing from it and no other module reads its result.

That work already exists as a **pen routine** (`docs/decisions/pen-routines.md`): scope, cadence,
assignee, questions, per-question and task-wide proof, verifier review, versioning, the kernel
materializer and roll-forward are all authored on `/routines`. What a routine could NOT say is
**where it appears on the phone**: every routine landed in the one "Routines" tab. This decision
adds that, and nothing else.

A **phone tab** (`pen_routine_tabs`, migration `000463`) is authored on `/routines` → Phone tabs:

| Field | Meaning |
|---|---|
| Label | The bar item's name and the list screen's title (max 24 characters). |
| Module | The phone module whose bottom bar carries it: Routines, Preventive Care, Feed, Weighing, Vaccination, Herd Operations, Milk, Health. Closed list, `domain.TabModules`. |
| Icon | One of the closed set the app ships (`domain.TabIcons`, mirrored by `MeshaIcons.forTabIcon`). |
| Filters | Which list controls the tab offers: Pending / Completed, Date, Pen. |
| Routines | Which routines appear on it. A routine sits on ONE tab; ticking it on another moves it. |

A **complex module stays coded**: feed direction, packing, transport, distribution, wastage,
weighing, vaccination, PC Care's own categories. Each owns tables, grains and rules (a per-bag
packing video, blind weighing, the vaccine kernel) that no form can express. Moving them onto the
generic screen is explicitly out of scope.

## How it reaches the phone

1. The bootstrap asks pen routines which tabs this person's bar carries
   (`PhoneTabsFor`: active tabs holding at least one un-retired routine the person owes, under the
   SAME assignee resolution as the task list) and places each one in its module's bar, before
   "You" (`workforce/app.placeModuleTabs`). A tab whose module this person is not served falls
   back to their Routines bar -- they hold that module, because they owe a routine -- so the work
   is never stranded. The item is `{key: routine_tab_<key>, label, href: /pen-routines/tab/<key>,
   icon}`; `icon` is the one new bar-item field and is carried only by these items.
2. The phone hosts ONE parameterized root, `/pen-routines/tab/{tab_key}`, rendering the existing
   routine list bound to the tab. `GET /app/pen-routines?tab=<key>` narrows rows, chip counts and
   pen options to that tab's routines and answers `tab {key, label, filters}` so the screen shows
   exactly the controls authored. `due_from`/`due_to`/`pen` are the date and pen filters. The
   detail, capture, submit and verifier review are the routine's, unchanged.
3. Retiring a tab removes it from every bar; its routines keep raising work and read in Routines.

## The rule this changes (recorded as a maintainer decision)

Until now the phone's bar was a list in Go (`moduleNavRegistry`) and "giving a module is always a
code change". That stays true for modules and for every coded tab. What changes: **a phone tab
defined on the web is data**, composed into a module's bar at bootstrap. Three things keep it from
eroding the nav rules:

- It is **never a role-keyed template**: who gets it is who owes the work on it, the task-list
  predicate, per person.
- The **icon and module are closed vocabularies** in Go, with a test on each side, so the
  "every backend nav key has an explicit Android display mapping" rule holds by construction.
- It rides the **existing permission**: `pen_routines.execute` for the bar item and the list,
  `pen_routines.configure` to author. No new permission, no new verification category (a tab's
  work is verified as a routine, `pen_routine`).

## Known limits (phase 2, not built)

- The verifier sees these items under Routines, named by the routine, not under the host module.
- The assignable people are the routine vocabulary (park head, directors, CXO). Ground staff on
  the new department manager roles cannot be picked until that vocabulary is widened -- a separate
  decision.
- No badge count on a web-defined tab yet.

Pinned by `domain.TestTabValidationRefusesAnythingThePhoneCannotRender`,
`domain.TestTabKeyIsARouteSafeSlugOfTheLabel`,
`workforce/app.TestWebDefinedTabsLandInTheirModuleBarBeforeYou`,
`http.TestListOpenedFromAPhoneTabNarrowsAndNamesTheTab`,
`http.TestTabRoutesDecodeAndServeTheVocabulary`, and on real Postgres
`TestPenRoutinePhoneTabsPlaceNarrowAndReachOnlyTheAssigneePostgres`.

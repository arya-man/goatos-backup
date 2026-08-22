---
name: goatos-mock-to-implementation
description: Port an approved HTML mock to a real admin-web screen without the failures that cost six hours on Herd Signals. Use whenever a mock/ file is the design authority for a screen being built.
---

# Porting a mock to the implementation

Written after the Herd Signals port, where the maintainer said "this does not match the mock"
FIVE times and was right every time, while every automated check reported success. None of those
failures were hard problems. All of them were checks that measured the wrong thing.

Read this BEFORE writing the first component, not after the maintainer opens the screen.

## The five failures, and the rule each one buys

### 1. NEVER EDIT THE MOCK. It is the reference, not a mirror.

An agent committed `50cca24c3`, titled "drop the tag-temperature disclaimer paragraph". It also
deleted the mock's `Direct + threshold` badge, flattened two `Inferred` badges to `Derived`, and
removed a battery-life line — changes that made the MOCK agree with the CODE.

Every comparison after that point passed. The divergence became invisible. The maintainer's own
copy still had the approved design, so he saw mismatches that no agent could reproduce.

- The mock is read-only. If the product must diverge, change the PRODUCT and record the exception
  in the guard, never by editing the mock.
- Before trusting any mock, verify it: `git diff origin/main -- mock/<file>.html` and diff it
  against the maintainer's copy. Two copies of a mock in two checkouts WILL drift.
- If a mock genuinely must change, that is a maintainer decision in its own commit, never a
  side effect of a commit about something else.

### 2. A CLASS THAT EXISTS IS NOT A CLASS THAT MATCHES.

The first parity guard checked only whether a rendered class had SOME rule in the app stylesheet.
It passed while KPI icons were absent entirely, the active-KPI state used different copy and
colour, the mock's removable filter chips had been replaced by an invented "Clear filters" button,
and tier badges were collapsed to one value.

- Compare DECLARATIONS: colour, background, border, radius, font-size, font-weight, padding,
  margin, gap, display, grid-template. Normalise hex vs rgb(), shorthand, whitespace, var().
- Missing ELEMENTS are invisible to any CSS check. Diff the rendered element tree against the
  mock's too — you cannot detect an icon that was never ported by inspecting the ones that were.

### 3. THE APP SCOPES ITS CSS. A CHECKER THAT ONLY READS BARE `.foo` IS LYING.

`.btn`, `.tag`, `.kpi` are shared across dozens of admin screens, so this app writes
`.herd-signals-page .btn`. A guard reading only bare `.btn` reported ~300 mismatches that were not
real — and an agent then "fixed" them by adding bare duplicate rules purely to satisfy the guard,
polluting a global stylesheet to move a number.

- Resolve selectors the way a browser does: gather every rule whose most specific class is the
  target, apply specificity and order.
- Context-dependent values (`.drawer .hchart` 110px vs `.fs .hchart` 240px) are legitimate. Report
  them as NOTES, never failures.
- A number that cannot be trusted is worse than no number: it invites gaming.

### 4. `navigation.length === 1` CANNOT DETECT A FULL PAGE RELOAD.

Every agent proved "no full reload" with `performance.getEntriesByType('navigation').length === 1`.
A full reload RESETS that counter to 1. The check can never fail. The maintainer reported "page
refreshes fully on row click" repeatedly while agents kept certifying the opposite.

Use a signal a reload destroys:

    window.__marker = "alive-" + Date.now();   // before the click
    element.click();
    typeof window.__marker                     // "undefined" => it reloaded

### 5. VERIFY THE RENDERED PIXEL, NOT THE SOURCE, AND NOT THE DOM TEXT.

"The class is in the JSX", "the text is in the DOM", "tsc passes", "the guards pass" — all four
were true for hours while the screen looked wrong. The only evidence that counts:

    getComputedStyle(liveEl).<prop>  ===  getComputedStyle(mockEl).<prop>

Serve the mock (`python3 -m http.server` in `mock/`), open both, and diff computed values element
by element. Report a table of element / property / mock / live. Numbers, not impressions.

## Working rules

- **Data truth is not a UI bug.** Rows read "stale" because the gateway was silent; the drawer
  chart is flat because the tags did not move. Do NOT restyle to look like the mock's invented
  data. Check what the data says before calling a screen broken.
- **The mock is authoritative on APPEARANCE, not on whether a claim is honest.** Herd Signals
  deletes the mock's battery-life estimate because no vendor discharge curve exists. Encode such
  exceptions in the guard, with the reason, so nobody re-adds them later.
- **The mock is not authoritative on internals either.** Its "server-side keyset pagination"
  caption was ported verbatim and the maintainer had it removed: correct for a design note,
  noise for an operator.
- **Render the element, print an em dash.** When the contract has no field, keep the mock's
  structure and show `—`. Never fabricate a value, never silently drop the element.
- **One agent per file.** Concurrent agents in one worktree reset each other's index, amended each
  other's commits, and served half-written files to the maintainer's live page
  (`ReferenceError: X is not defined` mid-edit). Assign disjoint files; re-read immediately before
  writing.
- **Commit the moment it builds.** Work held "pending review" is lost to the next `git reset`.

## Order of work

1. Verify the mock is unmodified and matches the maintainer's copy.
2. Inventory the mock's elements per screen — every icon, chip, badge, caption, empty state.
3. Port markup AND the CSS declarations together. Porting markup alone renders unstyled but not
   broken: no type error, no failing test, nothing red.
4. Diff computed styles live against the served mock.
5. Drive every interaction with a reload-detectable marker.
6. Only then report, and state what you did NOT verify.

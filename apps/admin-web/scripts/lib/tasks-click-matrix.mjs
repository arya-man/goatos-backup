/**
 * Pure helpers for `scripts/smoke-tasks-click-matrix-live.mjs`. Nothing here touches Playwright,
 * so every decision the harness makes about an element — click it, type into it, guard it as a
 * write, assert it absent — is unit-testable without a browser.
 */

/** A page that says any of these has not "rendered", whatever the screenshot shows. */
export const FAILURE_STRINGS = [
  "Something went wrong",
  "This screen failed to render",
  "failed to render",
  "contract unavailable",
  "missing copy key",
  "Application error",
  "Unhandled Runtime Error",
  "This page couldn't load",
  "invalid_bearer_token",
  "backend_down",
  "route_not_registered",
];

/** Phone tap-target floor, in CSS px. The page's own phone-viewport test uses the same number. */
export const PHONE_TAP_FLOOR = 40;

export function findFailureString(text) {
  const haystack = String(text ?? "");
  return FAILURE_STRINGS.find((needle) => haystack.includes(needle)) ?? null;
}

/**
 * A stable identity for an element ACROSS stages and viewports, so the harness can tell "hidden
 * here, activated there" from "hidden everywhere, never exercised". Deliberately drops `on`,
 * `set`, `open`, `is-*` state classes and the per-row ids in hrefs.
 */
export function elementKey(descriptor) {
  const classes = String(descriptor.className ?? "")
    .split(/\s+/)
    .filter((c) => c && !["on", "set", "open", "p"].includes(c) && !c.startsWith("is-"))
    .sort()
    .join(".");
  // Only the START of the label: a disclosure reads "Deadline" closed and "Deadline any" or
  // "Deadline 01/09/2026–…" once a value is shown, and it is still the same control.
  const name = String(descriptor.name ?? "")
    .replace(/\d+/g, "#")
    .slice(0, 8);
  return `${descriptor.tag}${descriptor.role ? `[${descriptor.role}]` : ""}${descriptor.type ? `[${descriptor.type}]` : ""}.${classes}:${name}`;
}

/**
 * Decide HOW the harness activates a described element, or why it does not.
 *
 * Returns `{ how, reason }` where `how` is one of:
 *   hidden       — not rendered at this viewport: recorded, counted for coverage, not clicked
 *   covered      — while an overlay is open (a stage that opened one, OR a page that rendered
 *                  one itself: the detail drawer is `aria-modal` on its URL), an element the
 *                  overlay's scrim sits on top of: asserted unreachable (a modal that let clicks
 *                  through would be the defect), not clicked
 *   current      — a `role=tab` that is already `aria-selected`: re-clicking the selected tab
 *                  is a no-op by design, not a dead control; the OTHER tabs prove the control
 *   disabled     — asserted disabled/aria-disabled instead of clicked (a pager's Prev on page 1)
 *   drag-native  — a native HTML5 `draggable="true"` element: a FAIL at every width. The board
 *                  drags with dnd-kit (mouse, touch, keyboard; guard task-board-touch-dnd), and a
 *                  native drag is the retired path touch browsers never fire. The real drag is
 *                  exercised by scripts/task-board-dnd-e2e.mjs, never by this click matrix.
 *   select       — choose a different option
 *   type         — fill text; a value change alone is enough ONLY for composer fields (the
 *                  commit control is asserted separately); the toolbar search must fire a request
 *   date         — fill a date; value change alone is enough (Apply commits it)
 *   toggle       — checkbox/radio: click, expect checked/aria-checked to change
 *   file         — asserted present; no upload against live data
 *   write-guard  — a submit button on a VALID form: it would persist against the shared DB, so
 *                  it is asserted enabled and reachable, and NOT submitted
 *   submit-invalid — a submit button on an INVALID form: clicked; the browser must refuse and
 *                  focus the offending field, and no request may fire
 *   click        — everything else
 */
export function planActivation(descriptor, viewportWidth, overlayStage = false) {
  if (descriptor.hidden) return { how: "hidden", reason: "not rendered at this viewport" };
  // The page can open its own overlay from the URL (`?task=` renders the modal drawer), so the
  // enumeration reports whether one was open (`descriptor.overlay`) and the stage need not know.
  if (descriptor.covered && (overlayStage || descriptor.overlay)) return { how: "covered", reason: "behind the open overlay's scrim: asserted unreachable while it is open" };
  if (descriptor.disabled) return { how: "disabled", reason: "asserted disabled/aria-disabled rather than clicked" };
  if (descriptor.role === "tab" && descriptor.selected) return { how: "current", reason: "the already-selected tab: re-clicking it is a no-op by design (its siblings prove the control)" };
  if (descriptor.draggable === "true") {
    return { how: "drag-native", reason: "a native HTML5 draggable is the retired board drag (touch never fires it); the board drags with dnd-kit" };
  }
  const tag = descriptor.tag;
  const type = descriptor.type || "";
  if (tag === "select") return { how: "select", reason: "choose a different option" };
  if (tag === "input" && ["date", "datetime-local", "time", "month", "week"].includes(type)) return { how: "date", reason: `fill a ${type}` };
  if (tag === "input" && (type === "checkbox" || type === "radio")) return { how: "toggle", reason: "toggle it" };
  if (tag === "input" && type === "file") return { how: "file", reason: "file input: asserted present and enabled (no upload performed on live data)" };
  if (tag === "input" && type === "hidden") return { how: "hidden", reason: "hidden input carries form state; not a control" };
  if (tag === "textarea" || (tag === "input" && ["search", "text", "", "url", "email", "number"].includes(type))) {
    return { how: "type", reason: "type into it" };
  }
  if (tag === "button" && type === "submit") {
    return descriptor.formValid
      ? { how: "write-guard", reason: "submit on a valid form would persist against the shared DB: asserted enabled, not submitted" }
      : { how: "submit-invalid", reason: "submit on an invalid form: the browser must refuse and focus the field, and nothing may be sent" };
  }
  return { how: "click", reason: "click" };
}

/**
 * The keys of the observable state that changed. `ignore` lists keys that do NOT count as a
 * change for this activation — typing into a field trivially changes `inputs`, so a typed
 * control must show something else.
 */
export function observedChange(before, after, ignore = []) {
  if (!before || !after) return [];
  const skip = new Set(ignore);
  return Object.keys(after).filter((key) => !skip.has(key) && JSON.stringify(after[key]) !== JSON.stringify(before[key]));
}

/** True when this activation must show more than its own field value. */
export function requiresBeyondValue(descriptor) {
  // The toolbar search is debounced into a navigation: its value changing is not the point.
  return descriptor.tag === "input" && descriptor.type === "search" && !descriptor.role;
}

/**
 * Elements that were hidden at some viewport and were never activated at ANY viewport. A hidden
 * element that is activated elsewhere is a legitimate "absent here" (the phone's Close-filters
 * button at desktop); one activated nowhere is a control the matrix never proved.
 */
export function coverageGaps(results) {
  const activated = new Set();
  const hidden = new Map();
  for (const entry of results) {
    if (!entry.key) continue;
    if (entry.how === "hidden") {
      if (!hidden.has(entry.key)) hidden.set(entry.key, entry);
    } else if (entry.status === "pass" && !["disabled", "covered", "sampled", "coverage", "current"].includes(entry.how)) {
      activated.add(entry.key);
    }
  }
  return [...hidden.entries()].filter(([key]) => !activated.has(key)).map(([, entry]) => entry);
}

/** Pass/fail counts per viewport, for the readable summary. */
export function summarize(results) {
  const out = {};
  for (const entry of results) {
    const bucket = (out[entry.viewport] ??= { pass: 0, fail: 0, total: 0 });
    bucket.total += 1;
    bucket[entry.status === "pass" ? "pass" : "fail"] += 1;
  }
  return out;
}

export function renderMarkdown({ runId, appBaseUrl, results }) {
  const summary = summarize(results);
  const lines = ["# Tasks Desk Click Matrix", "", `Run: \`${runId}\``, `Admin web: \`${appBaseUrl}\``, ""];
  for (const [viewport, counts] of Object.entries(summary)) {
    lines.push(`- ${viewport}: ${counts.pass} pass / ${counts.fail} fail (${counts.total})`);
  }
  lines.push("", "| Status | Viewport | Stage | Element | Action | Observed |", "| --- | --- | --- | --- | --- | --- |");
  for (const entry of results) {
    lines.push(
      `| ${entry.status} | ${entry.viewport} | ${entry.stage} | ${cell(entry.element)} | ${cell(entry.how ?? "")} | ${cell(entry.detail ?? "")} |`,
    );
  }
  return `${lines.join("\n")}\n`;
}

function cell(value) {
  return String(value).replaceAll("|", "\\|").replaceAll("\n", " ");
}

export function trimTrailingSlash(value) {
  return String(value).replace(/\/+$/, "");
}

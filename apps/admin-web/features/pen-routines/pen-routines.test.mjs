import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import test from "node:test";

import { decodeEvidence, decodePenRoutineWrite, FORM_JSON_FIELDS, slugQuestionId } from "./routine-form-model.ts";

const here = new URL("./", import.meta.url);
const read = (rel) => readFileSync(new URL(rel, here), "utf8");
const page = read("../../app/(admin)/routines/page.tsx");
const feature = read("./routines-page.tsx");
const drawer = read("./routine-drawer.tsx");
const filter = read("./routine-filter.tsx");
const actions = read("./pen-routine-actions.ts");
const featureFiles = readdirSync(here).filter((name) => name.endsWith(".tsx"));

// /routines is a contract-backed page (maintainer instruction 2026-09-16): the route reads the
// "pen-routines" contract and the feature renders its copy, tables and controls from it.
test("the routines page reads the pen-routines contract and forces dynamic rendering", () => {
  assert.match(page, /requireAdminWebPageContract\("pen-routines"\)/);
  assert.match(page, /export const dynamic = "force-dynamic"/);
  assert.match(feature, /table\(pageContract, "pen-routines"\)/);
  assert.match(feature, /table\(pageContract, "pen-routine-tasks"\)/);
});

// Every word a reader sees comes from `copy(...)` or backend data -- and it says PEN, never SHED
// (docs/decisions/pen-not-shed-vocabulary.md). The JSX text-node scan mirrors
// features/counts/pen-vocabulary.test.mjs; the literal scan mirrors check-ui-contract-literals.
test("no routines JSX text node or visible attribute says shed or carries local copy", () => {
  const SHED = /\b[Ss]heds?\b/;
  const offences = [];
  for (const name of featureFiles) {
    const lines = read(`./${name}`).split("\n");
    lines.forEach((line, i) => {
      const trimmed = line.trim();
      if (trimmed.startsWith("//") || trimmed.startsWith("*") || trimmed.startsWith("{/*") || trimmed.startsWith("/*")) return;
      for (const match of line.matchAll(/>([^<>{}"']*?)</g)) {
        const text = match[1].trim();
        if (text && SHED.test(text)) offences.push(`${name}:${i + 1} shed in text: ${text}`);
        // A text node with letters is local copy; punctuation-only runs (separators, arrows) are layout.
        if (text && /[A-Za-z]{3,}/.test(text)) offences.push(`${name}:${i + 1} local text: ${text}`);
      }
      for (const match of line.matchAll(/\b(?:placeholder|aria-label|title)=["']([^"']+)["']/g)) {
        offences.push(`${name}:${i + 1} literal attribute copy: ${match[1]}`);
      }
    });
  }
  assert.deepEqual(offences, []);
});

test("pen labels render only the backend-composed operational_location_display", () => {
  assert.match(feature, /row\.operational_location_display/);
  assert.match(drawer, /pen\.operational_location_display/);
  for (const name of featureFiles) {
    const source = read(`./${name}`);
    assert.doesNotMatch(source, /shed_name\s*\+|\$\{[^}]*shed_name[^}]*\}\s*-/, `${name} must not compose shed + partition`);
    assert.doesNotMatch(source, /operationalLocationLabel\(/, `${name} must render the backend display, not re-derive it`);
  }
});

// Controls are gated on BOTH halves: the page renders New / Edit / status only when the contract
// enables the control, and shows the contract's own disabled reason otherwise -- never a role string.
test("authoring controls render only when the contract enables them, else the backend reason", () => {
  assert.match(feature, /controlEnabled\(pageContract, "create_routine", false\)/);
  assert.match(feature, /controlEnabled\(pageContract, "edit_routine", false\)/);
  assert.match(feature, /controlEnabled\(pageContract, "set_routine_status", false\)/);
  assert.match(feature, /\{canCreate && catalogParkId \? \(\s*<LocalOverlayLink/);
  assert.match(feature, /\{!canConfigure \? <div className="note"[^>]*>\{c\("configure\.disabled_no_access"\)\}<\/div> : null\}/);
  assert.match(drawer, /readOnly \? \(?\s*<div className="note">\{copy\(pageContract, "configure\.disabled_no_access"\)\}/);
  assert.match(drawer, /isEdit && canSetStatus && routine\.status !== "retired"/);
  for (const source of [feature, drawer, filter, actions]) {
    assert.doesNotMatch(source, /ceo_internal|park_head|pc_director|role ===|\.role\b/, "no role-string conditional");
  }
});

// The drawer is client-local (LocalOverlayLink + LocalOverlayDrawer) and the writes land in place.
test("the drawer opens locally and every write lands in place through useActionState", () => {
  assert.match(feature, /<LocalOverlayDrawer/);
  assert.match(feature, /selectionKey=\{PARAM_EDIT\}/);
  assert.notEqual(feature.match(/PARAM_EDIT = "([a-z_]+)"/)[1], feature.match(/PARAM_ROUTINE = "([a-z_]+)"/)[1], "drawer param must differ from the task filter param");
  assert.match(drawer, /useActionState\(isEdit \? updateRoutineAction : createRoutineAction, INITIAL\)/);
  assert.match(drawer, /useActionState\(setRoutineStatusAction, INITIAL\)/);
  assert.doesNotMatch(actions, /redirect\(|return_to|\?notice=/);
  assert.match(actions, /revalidatePath\(ROUTINES_PATH\)/);
  assert.match(actions, /randomUUID\(\)/);
  for (const name of ["createRoutineAction", "updateRoutineAction", "setRoutineStatusAction"]) {
    assert.match(actions, new RegExp(`export async function ${name}\\(previous: PenRoutineActionState, formData: FormData\\): Promise<PenRoutineActionState>`));
  }
  // The backend's own refusal sentence rides through verbatim; no sentence is composed here.
  assert.match(actions, /result\.error\.message/);
  assert.doesNotMatch(actions, /["'][A-Z][a-z]+ [a-z]+[^"']*["']/, "no visible sentence literal in the actions");
});

function form(fields) {
  const fd = new FormData();
  for (const [key, value] of Object.entries(fields)) fd.set(key, typeof value === "string" ? value : JSON.stringify(value));
  return fd;
}

const base = {
  park_id: "11111111-1111-1111-1111-111111111111",
  name: "Pen cleaning",
  instruction: "Walk every pen before nine.",
  scope_kind: "all_pens",
  occupied_only: "on",
  cadence_kind: "daily",
  due_offset_days: "0",
  notify_time: "07:00",
  review_kind: "verifier",
  start_date: "2026-09-17",
  interval_days: "5",
  [FORM_JSON_FIELDS.assigneeRoles]: ["park_head", "ceo_internal"],
  [FORM_JSON_FIELDS.evidence]: {
    questions: [
      { id: "cleaned", kind: "yes_no", title: "Was the pen cleaned?", required: true },
      { id: "water", kind: "choice", title: "Water trough", required: true, options: [{ value: "clean", label: "Clean" }, { value: "dirty", label: "Dirty" }] },
      { id: "count", kind: "number", title: "Sick animals seen", required: false, min: "0", max: "500", unit: "head" },
      { id: "note", kind: "text", title: "Anything else", required: false, hint: "Optional" },
    ],
    photo: { min: "1", max: "3" },
    video: { min: 0, max: 1 },
    presence: "required",
  },
};

test("decoder: daily all-pens routine with a full evidence block round-trips as numbers, not strings", () => {
  const body = decodePenRoutineWrite(form(base));
  assert.equal(body.park_id, base.park_id);
  assert.equal(body.name, "Pen cleaning");
  assert.equal(body.instruction, "Walk every pen before nine.");
  assert.equal(body.scope_kind, "all_pens");
  assert.equal(body.occupied_only, true);
  assert.equal(body.pens, undefined, "all_pens carries no pen list");
  assert.equal(body.cadence_kind, "daily");
  assert.equal(body.weekdays, undefined);
  assert.equal(body.month_days, undefined);
  assert.equal(body.after_work_kinds, undefined);
  assert.strictEqual(body.due_offset_days, 0);
  assert.equal(body.notify_time, "07:00");
  assert.equal(body.review_kind, "verifier");
  assert.deepEqual(body.assignee_roles, ["park_head", "ceo_internal"]);
  assert.equal(body.start_date, "2026-09-17");
  assert.strictEqual(body.interval_days, null, "a daily routine carries no interval, whatever the form still held");
  assert.equal("assignee_user_ids" in body, false, "named people are gone from the write");
  assert.equal(body.row_version, undefined, "create carries no fence");
  assert.deepEqual(body.evidence.photo, { min: 1, max: 3 });
  assert.deepEqual(body.evidence.video, { min: 0, max: 1 });
  assert.equal(body.evidence.presence, "required");
  assert.equal(body.evidence.questions.length, 4);
  const [yesNo, choice, number, text] = body.evidence.questions;
  assert.deepEqual(yesNo, { id: "cleaned", kind: "yes_no", title: "Was the pen cleaned?", required: true });
  assert.deepEqual(choice.options, [{ value: "clean", label: "Clean" }, { value: "dirty", label: "Dirty" }]);
  assert.strictEqual(number.min, 0);
  assert.strictEqual(number.max, 500);
  assert.equal(number.unit, "head");
  assert.equal(number.options, undefined, "a number question carries no options");
  assert.equal(text.hint, "Optional");
  assert.equal(text.required, false);
});

test("decoder: weekly carries weekdays, monthly carries month days UNCLAMPED, after_work carries kinds", () => {
  const weekly = decodePenRoutineWrite(form({ ...base, cadence_kind: "weekly", [FORM_JSON_FIELDS.weekdays]: [1, 3, 5] }));
  assert.deepEqual(weekly.weekdays, [1, 3, 5]);
  assert.equal(weekly.month_days, undefined);

  // 31 on a 30-day month means the last day -- the backend's clamp, never the form's.
  const monthly = decodePenRoutineWrite(form({ ...base, cadence_kind: "monthly", [FORM_JSON_FIELDS.monthDays]: [1, 15, 31] }));
  assert.deepEqual(monthly.month_days, [1, 15, 31]);
  assert.equal(monthly.weekdays, undefined);

  const afterWork = decodePenRoutineWrite(form({ ...base, cadence_kind: "after_work", due_offset_days: "1", [FORM_JSON_FIELDS.afterWorkKinds]: ["deworming", "weighing"] }));
  assert.deepEqual(afterWork.after_work_kinds, ["deworming", "weighing"]);
  assert.strictEqual(afterWork.due_offset_days, 1);
});

test("decoder: selected pens carry shed + partition, an undivided shed carries no partition key", () => {
  const body = decodePenRoutineWrite(
    form({
      ...base,
      scope_kind: "selected_pens",
      occupied_only: "off",
      row_version: "7",
      [FORM_JSON_FIELDS.pens]: [
        { shed_id: "s1", partition_label: "Part 3" },
        { shed_id: "s2", partition_label: "" },
      ],
    }),
  );
  assert.equal(body.occupied_only, false);
  assert.deepEqual(body.pens, [{ shed_id: "s1", partition_label: "Part 3" }, { shed_id: "s2" }]);
  assert.strictEqual(body.row_version, 7);
});

test("decoder: roles round-trip and an unknown role key is never sent", () => {
  const body = decodePenRoutineWrite(form({ ...base, [FORM_JSON_FIELDS.assigneeRoles]: ["pc_director", "breeding_director", "janitor", "", "ceo_internal"] }));
  assert.deepEqual(body.assignee_roles, ["pc_director", "breeding_director", "ceo_internal"]);
  const none = decodePenRoutineWrite(form({ ...base, [FORM_JSON_FIELDS.assigneeRoles]: "not json" }));
  assert.deepEqual(none.assignee_roles, [], "an empty list goes to the backend, which refuses it as no_roles");
});

test("decoder: every_n_days carries its interval as a number; blank stays null; a bad start date is dropped", () => {
  const every = decodePenRoutineWrite(form({ ...base, cadence_kind: "every_n_days", interval_days: "3" }));
  assert.equal(every.cadence_kind, "every_n_days");
  assert.strictEqual(every.interval_days, 3);
  assert.equal(every.weekdays, undefined);
  const blank = decodePenRoutineWrite(form({ ...base, cadence_kind: "every_n_days", interval_days: "" }));
  assert.strictEqual(blank.interval_days, null);
  const weekly = decodePenRoutineWrite(form({ ...base, cadence_kind: "weekly", interval_days: "3", [FORM_JSON_FIELDS.weekdays]: [2] }));
  assert.strictEqual(weekly.interval_days, null);
  const noStart = decodePenRoutineWrite(form({ ...base, start_date: "17/09/2026" }));
  assert.equal(noStart.start_date, undefined, "absent keeps the stored date on update, today on create");
});

test("decoder: a whole-park task clears pens and never skips empty pens", () => {
  const body = decodePenRoutineWrite(
    form({ ...base, scope_kind: "park", occupied_only: "on", [FORM_JSON_FIELDS.pens]: [{ shed_id: "s1", partition_label: "Part 3" }] }),
  );
  assert.equal(body.scope_kind, "park");
  assert.deepEqual(body.pens, []);
  assert.equal(body.occupied_only, false);
});

test("the drawer assigns by role, offers a whole-park scope and every few days, and the tables render roles and park tasks", () => {
  assert.match(drawer, /catalog\?\.roles/);
  assert.match(drawer, /copy\(pageContract, "empty\.role_people"\)/);
  assert.match(drawer, /copy\(pageContract, "hint\.assignee_roles"\)/);
  assert.match(drawer, /copy\(pageContract, "hint\.park_scope"\)/);
  assert.match(drawer, /field\("interval_days"\)/);
  assert.match(drawer, /field\("start_date"\)/);
  assert.match(drawer, /(option\.key|key) === "after_work" && draft\.scopeKind === "park"/, "after work is unavailable for a whole-park task");
  assert.doesNotMatch(drawer, /assignee_user_ids|field\("assignees"\)|hint\.assignees/);
  assert.match(feature, /routine\.assignee_roles\.map\(\(option\) => option\.label\)/);
  assert.match(feature, /c\("table\.people\.preview"\)/);
  assert.match(feature, /row\.scope_kind === "park" \|\| !row\.operational_location_display \? row\.park_name/);
});

test("decoder: presence defaults to off and proof rules to zero when the evidence block is blank", () => {
  const evidence = decodeEvidence({});
  assert.deepEqual(evidence, { questions: [], photo: { min: 0, max: 0 }, video: { min: 0, max: 0 }, presence: "off" });
  const body = decodePenRoutineWrite(form({ ...base, [FORM_JSON_FIELDS.evidence]: "not json" }));
  assert.equal(body.evidence.presence, "off");
});

test("decoder: a missing park or name is refused before any request is made", () => {
  assert.throws(() => decodePenRoutineWrite(form({ ...base, name: "  " })));
  assert.throws(() => decodePenRoutineWrite(form({ ...base, park_id: "" })));
});

test("question ids derive from the title as letters, digits and underscores", () => {
  assert.equal(slugQuestionId("Was the pen cleaned?"), "was_the_pen_cleaned");
  assert.equal(slugQuestionId("  Water trough / level  "), "water_trough_level");
  assert.equal(slugQuestionId("!!!"), "");
});

test("decoder: a question's proof travels only as a recognised medium; none/blank/unknown sends no proof block", () => {
  const evidence = decodeEvidence({
    questions: [
      { id: "clean", kind: "yes_no", title: "Pen cleaned?", required: true, proof: { kind: "photo", count: "multiple" } },
      { id: "water", kind: "text", title: "Water", required: false, proof: { kind: "video" } },
      { id: "gate", kind: "yes_no", title: "Gate", required: true, proof: { kind: "none", count: "single" } },
      { id: "feed", kind: "yes_no", title: "Feed", required: true, proof: { kind: "audio", count: "single" } },
      { id: "plain", kind: "yes_no", title: "Plain", required: true },
    ],
    photo: { min: 0, max: 0 },
    video: { min: 0, max: 0 },
    presence: "off",
  });
  assert.deepEqual(evidence.questions[0].proof, { kind: "photo", count: "multiple" });
  assert.deepEqual(evidence.questions[1].proof, { kind: "video", count: "single" }, "a missing count defaults to single");
  assert.equal("proof" in evidence.questions[2], false, "none is the catalog's key for no proof and never travels");
  assert.equal("proof" in evidence.questions[3], false, "an unknown medium is dropped, not guessed");
  assert.equal("proof" in evidence.questions[4], false);
});

test("the drawer offers per-question proof from the catalog's own vocabulary, never a local list", () => {
  const drawer = readFileSync(new URL("./routine-drawer.tsx", import.meta.url), "utf8");
  assert.match(drawer, /catalog\?\.question_proof_kinds/);
  assert.match(drawer, /catalog\?\.question_proof_counts/);
  assert.match(drawer, /"field\.question_proof"/);
  assert.match(drawer, /"field\.question_proof_count"/);
  assert.doesNotMatch(drawer, /<option[^>]*>\s*(Photo|Video|Photo or video|No proof)\s*<\/option>/, "proof option labels are backend copy");
});

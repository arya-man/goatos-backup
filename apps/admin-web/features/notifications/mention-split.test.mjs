// The composer is split across the line where JavaScript stops being optional, and the split is
// what a reader will most easily undo. The textarea and its hidden mentions input are eager
// because WITHOUT JS the textarea still submits its text; the @-picker is fetched on first use
// because it was 4.6 KB gzip on every one of the 63 routes to render a popup almost none of them
// open. A `lazy()` around the WHOLE component would take the field out of the server-rendered
// HTML and break the no-JS submit; a static import of the picker (or of its rulebook) from the
// eager half would silently put the kilobytes back on every route, with nothing failing.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
const field = read("./mention-textarea.tsx");
const picker = read("./mention-picker.tsx");
const barrel = read("./index.ts");
const eagerRules = read("./mention-value.ts");

// ---- The field is eager and complete: a named textarea plus the hidden ids input, rendered
// unconditionally so they are both in the HTML the server sends.
assert.match(field, /<textarea/, "the eager half must render the real textarea");
assert.match(field, /name=\{name\}/, "the textarea must carry its form field name");
assert.match(
  field,
  /<input\s+type="hidden"\s+name=\{mentionsName\}/,
  "the hidden mentions input must be rendered by the eager half, beside the textarea",
);
// Neither may sit behind the picker's load: a conditional or a Suspense fallback would remove
// them from the no-JS HTML.
const suspenseBody = field.slice(field.indexOf("<Suspense"));
assert.doesNotMatch(suspenseBody, /<textarea|name=\{mentionsName\}/, "the field must not be inside the Suspense boundary");

// ---- The picker is fetched, not imported.
assert.match(
  field,
  /lazy\(\(\) => import\("\.\/mention-picker"\)\)/,
  "the picker must arrive through a dynamic import, so it lands in its own chunk",
);
assert.doesNotMatch(
  field,
  /^import (?!type )[^\n]*from "\.\/mention-picker"/m,
  "a static import of the picker puts it back in every route's chunk",
);
assert.doesNotMatch(
  field,
  /from "\.\/mention-model/,
  "the eager half must not import the picker's rulebook -- that is the weight being deferred",
);
// The type-only import of the popup-state shape is fine (erased at build); assert it stays typed.
assert.match(field, /import type \{ MentionPopupState \}/, "the popup-state contract is a type-only import");

// ---- The eager rulebook is only the hidden input's contract, and stays import-free so the
// field's chunk pulls in nothing else.
assert.doesNotMatch(eagerRules, /^import /m, "the eager rules module must stay import-free");
for (const name of ["selectedMentionUserIds", "pruneMentionSelections", "mentionValue", "MENTION_TRIGGER"]) {
  assert.ok(eagerRules.includes(`export ${name.startsWith("MENTION") ? "const" : "function"} ${name}`), `${name} belongs to the eager half`);
}
// Query parsing, ranking, insertion and keyboard movement are the PICKER's, and must not creep back.
for (const name of ["activeMentionQuery", "filterMentionCandidates", "applyMentionSelection", "moveMentionHighlight"]) {
  assert.ok(!eagerRules.includes(`function ${name}`), `${name} is picker-only and must not be in the eager module`);
  assert.ok(field.indexOf(name) === -1, `the eager field must not call ${name}`);
}

// ---- The trigger is not swallowed: typing `@` is what starts the load, and the picker derives
// whether it is open from the field's current (text, caret) rather than from that keystroke.
assert.match(
  field,
  /includes\(MENTION_TRIGGER\)\) setPickerWanted\(true\)/,
  "the `@` that can open the popup must be the thing that fetches it",
);
assert.match(
  picker,
  /activeMentionQuery\(text, caret, completed\)/,
  "the picker must derive its query from the field's state, so an early `@` still opens it on arrival",
);
assert.doesNotMatch(picker, /<textarea/, "the picker must not render a field of its own");

// ---- The barrel must not re-export the picker: the shell imports this file, so a static
// re-export would drag it into the shared chunk of every route (the panel's lesson, one door over).
assert.doesNotMatch(barrel, /from "\.\/mention-picker"/, "the picker is a feature internal");
assert.doesNotMatch(
  barrel,
  /^export \{ (?!type )[^}]*\} from "\.\/mention-model"/m,
  "the barrel must re-export no VALUE from the picker's rulebook",
);

// The two write modals share ONE person picker and ONE deadline field with the rest of the desk.
// The "For" field was a native `<select>` listing job titles only ("CEO / CXO" twice), and the
// deadline a native `datetime-local` whose Chrome picker drew itself over the modal's buttons.
// These pin the replacements at the source level: the shared picker posts the field the Server
// Action reads, and the Server Action reads the split deadline the calendar + selects post.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
const newModal = read("./new-task-modal.tsx");
const editModal = read("./edit-task-modal.tsx");
const forms = read("./task-write-forms.tsx");
const actions = read("./actions.ts");

test("the New task 'For' field is the shared assignee picker in form mode, not a title-only select", () => {
  assert.doesNotMatch(newModal, /<select[^>]*name="assignee_user_id"/);
  assert.match(newModal, /import \{ AssigneePicker \} from "@\/components\/assignee-picker"/);
  assert.match(newModal, /<AssigneePicker\s+mode="single"\s+name="assignee_user_id"/);
  // Rows carry the person AND the title, so two people with one title stay distinct by name.
  assert.match(newModal, /title: assignee\.title/);
  // The action still reads the same field the old select posted.
  assert.match(actions, /formData\.get\("assignee_user_id"\)/);
  // The required check moved to the form, with the backend's own sentence.
  assert.match(newModal, /if \(!assigneeId\) \{\s*event\.preventDefault\(\)/);
  assert.match(newModal, /feedback\.missing_assignee/);
  // Nothing of the URL person filter is imported: one control, and it is the Work Board's.
  assert.doesNotMatch(newModal, /task-people-filter/);
});

test("both modals take the deadline from the console's calendar plus time selects", () => {
  for (const [name, source] of [["new", newModal], ["edit", editModal]]) {
    assert.doesNotMatch(source, /datetime-local/, `${name} modal must not use the native picker`);
    assert.match(source, /<TaskDeadlineFields/, `${name} modal uses the shared deadline field`);
  }
  assert.match(forms, /import \{ ThemedDatePicker \} from "@\/components\/themed-date-picker"/);
  assert.match(forms, /name="deadline_date"/);
  assert.match(forms, /name="deadline_hour"/);
  assert.match(forms, /name="deadline_minute"/);
  // Both Server Actions read the split fields through the one helper.
  assert.equal((actions.match(/farmDeadlineLocalFromForm\(formData\)/g) ?? []).length, 2);
  // The edit form keeps "leave it to keep the stored deadline": not required there.
  assert.match(editModal, /<TaskDeadlineFields[\s\S]*?required=\{false\}/);
  assert.match(newModal, /<TaskDeadlineFields[\s\S]*?required\s/);
});

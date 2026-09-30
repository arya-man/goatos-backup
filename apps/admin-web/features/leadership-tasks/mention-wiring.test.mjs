// The @-mention composer answers in TWO halves -- the prose and the picked ids -- and the halves
// travel in two separate form fields wired by NAME. A rename on one side is silent: the note still
// posts, the ids quietly become an empty list, and nobody is notified. These assert the names
// match, end to end, and that the ids are not collected where no endpoint accepts them.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");
// The composer is the in-place Activity composer (the retired `TaskCommentForm` in
// task-write-forms.tsx had no caller and was deleted with FIXJ3).
const commentForm = read("./task-activity-composer.tsx");
const actions = read("./actions.ts");
// The composer lives in the selected-task RAIL, which moved out of the page into its own
// component when the status board landed (2026-09-18). The candidate list still has to be handed
// in from outside, which is what this file asserts -- only the file that hands it in changed.
const detailPanel = read("./task-detail-panel.tsx");
const editModal = read("./edit-task-modal.tsx");

// ---- The composer's hidden ids field and the action's read must name the same key.
const posted = commentForm.match(/mentionsName="([^"]+)"/);
assert.ok(posted, "the update composer must post the picked ids in a named hidden field");
assert.ok(
  actions.includes(`formData.get("${posted[1]}")`),
  `the comment action must read the ids from "${posted[1]}" -- the field the form posts`,
);

// ---- The ids go to the backend as `mentions`, the shape LeadershipTaskCommentRequest declares.
// The server re-validates them, so the client sends ids and never a name.
assert.match(actions, /mentions\b/, "the comment action must send the picked ids as `mentions`");
assert.match(
  actions,
  /\{ user_id: userID \}/,
  "each mention travels as LeadershipTaskMentionRef ({ user_id }), never a display name",
);

// ---- Feature boundary: the composer arrives through the notifications barrel.
// tools/agent-hooks/check-boundaries.sh refuses a deep import into another feature's internals.
assert.match(
  commentForm,
  /from "@\/features\/notifications"/,
  "import the composer from the notifications barrel, never a deep path",
);
assert.doesNotMatch(commentForm, /features\/notifications\/[a-z]/, "no deep feature import");

// ---- The candidate list is passed IN (the component fetches nothing of its own).
assert.match(
  detailPanel,
  /mentionCandidates=\{assignees\}/,
  "the detail panel must hand the composer its candidate list",
);

// ---- The EDIT modal deliberately does NOT collect mention ids.
// `LeadershipTaskEditRequest` in contracts/openapi/app-api.yaml declares no `mentions` (only the
// comment request does), so a picker there would let a writer believe they had notified someone
// while nothing downstream reads the ids. Wire it when the edit endpoint accepts mentions.
assert.doesNotMatch(
  editModal,
  /mentionsName=/,
  "the edit modal must not collect mention ids while its endpoint declares no `mentions` field",
);

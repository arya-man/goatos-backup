/**
 * The wording of a REFUSED write on the Tasks desk: which contract key to ask for given which
 * facts resolved, and what the page says until that key is authored.
 *
 * NO IMPORTS, the same rule `task-url.ts` keeps and for the same two reasons. It lets
 * `task-board-dnd.test.mjs` exercise these rules under `node --test` without the `@/` path alias,
 * and it keeps the file directive-free so a server component could call it too — a plain function
 * exported from a `"use client"` module is a client REFERENCE, not a function (the defect
 * `task-presentation.ts` exists for). The contract lookup arrives as a FUNCTION so this module
 * never needs `@/lib/admin-ui-contract`.
 *
 * WHAT THIS FILE IS NOT: it is not the copy. Every sentence below is a FALLBACK — the third
 * argument of `copy(contract, key, fallback)` — and the page contract's own `feedback.*` key wins
 * the moment its owner authors it. What lives here is the SELECTION RULE plus the wording the
 * desk uses in the meantime.
 *
 * ── WHY A REFUSAL NAMES THINGS ────────────────────────────────────────────────────────────────
 * "Action could not be completed" was this page's wording for all three refusals, and it is
 * exactly the abstract copy AGENTS.md bans: a reader on a board of 424 tasks cannot act on it.
 * Two backend-supplied values are substituted into the sentence instead:
 *
 *   {status}  the task's current `status_chip`, from a re-read after the refusal — the backend's
 *             own chip wording ("In progress", "Done", "Cancelled"), verbatim. On a `version_conflict`
 *             this IS the point: the board is out of date and this is the truth it is out of date
 *             against. It is supplied only when the task genuinely MOVED (see `actions.ts`), so
 *             the sentence never tells a reader a task "is already Open" on a board saying Open.
 *   {name}    the person whose move it is, off the task itself: the assignee for a ladder move,
 *             the raiser for a cancel.
 *
 * NOTHING IS INVENTED. A value that did not resolve costs its CLAUSE — the sentence falls to the
 * variant that does not mention it — which is this repo's rule for an unresolvable identifier:
 * never a placeholder, never "someone", never a raw id.
 *
 * WHO MADE THE CHANGE is deliberately not claimed on a conflict. Neither the 409 envelope nor
 * `GET /app/leadership-tasks/{task_id}` reports the actor of the last status change, and deducing
 * one from the transition would be inventing a name. `actions.ts` records the backend follow-up
 * that would let the sentence name them.
 */

/** How this module reaches the page contract, without importing it. */
export type CopyLookup = (key: string, fallback: string) => string;

/**
 * The sentence for one outcome code, as specific as the resolved facts allow.
 *
 * Three keys per code, tried MOST specific first, so a missing fact costs a clause and never the
 * whole sentence:
 *   `feedback.<code>.named`   the name and the status both resolved
 *   `feedback.<code>.status`  only the status resolved
 *   `feedback.<code>`         neither — the code's own plain sentence
 * A template is used only when every placeholder it carries has a value, so an authored key can
 * never render the literal `{name}` at a reader. An unknown code returns "" and the banner falls
 * back to the contract's generic sentence rather than showing the raw token.
 */
export function refusalSentence(
  lookup: CopyLookup,
  code: string,
  statusNow?: string,
  who?: string,
): string {
  const name = (who ?? "").trim();
  const now = (statusNow ?? "").trim();
  const fallbacks = REFUSAL_FALLBACKS[code];

  if (name && now) {
    const filled = fill(
      lookup(`feedback.${code}.named`, fallbacks?.named ?? ""),
      name,
      now,
    );
    if (filled) return filled;
  }
  if (now) {
    const filled = fill(
      lookup(`feedback.${code}.status`, fallbacks?.status ?? ""),
      name,
      now,
    );
    if (filled) return filled;
  }
  if (name) {
    const filled = fill(
      lookup(`feedback.${code}.named`, fallbacks?.named ?? ""),
      name,
      now,
    );
    if (filled) return filled;
  }
  return lookup(`feedback.${code}`, fallbacks?.plain ?? "");
}

/** Substitutes the two values, and refuses a template whose placeholders it cannot all fill. */
function fill(template: string, name: string, now: string): string {
  if (!template) return "";
  if (template.includes("{name}") && !name) return "";
  if (template.includes("{status}") && !now) return "";
  return template.split("{name}").join(name).split("{status}").join(now);
}

/**
 * What the desk says until the contract's owner authors the keys above.
 *
 * Each names the thing that happened rather than reporting that something did. `version_conflict`
 * is worded as a statement of fact, not a promise: the action `revalidatePath`s and redirects, so
 * by the time the sentence is on screen the board has been re-read and shows the current version.
 */
export const REFUSAL_FALLBACKS: Record<
  string,
  { named?: string; status?: string; plain: string }
> = {
  version_conflict: {
    status:
      "This task was moved to {status} while this board was open, so your move was not applied. The board now shows that — move it from what you see now.",
    plain:
      "This task was changed while this board was open, so your move was not applied. The board now shows the current version.",
  },
  task_closed: {
    status: "This task is {status}, and a closed task cannot be moved.",
    plain: "This task is closed, so its status cannot be changed.",
  },
  not_assignee: {
    named: "Only {name}, the person this task is assigned to, can move it.",
    plain: "Only the person this task is assigned to can move it.",
  },
  not_raiser: {
    named: "Only {name}, who raised this task, can cancel it.",
    plain: "Only the person who raised this task can cancel it.",
  },
  invalid_status_transition: {
    status: "This task is {status}, and that is not a move it can make from there.",
    plain: "That is not a move this task can make from where it is now.",
  },
  task_not_found: { plain: "This task is no longer on the list." },
  not_on_task: { plain: "Only the people on this task, or someone named in a note on it, can write a note here." },
};

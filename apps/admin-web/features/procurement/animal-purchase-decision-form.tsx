"use client";

// The Accept / Reject form for ONE pending animal on /procurement/animal-purchases.
//
// Shown only for a pending row and only when the backend-declared `decide_animal_purchase`
// control is enabled — the server component decides both; this file never inspects a role. Every
// label arrives as a prop resolved from the page contract's copy map. Reject stays disabled until
// a note is typed (copy `decision.note` says so); Accept is one click. Both post the same Server
// Action with a derived idempotency key and the row_version fence.
//
// The decision lands IN PLACE (maintainer report 2026-09-14: "after accepting it scrolls up as if
// the whole page reloaded"): the action returns its outcome instead of redirecting, the form
// shows the backend's sentence beside the buttons, and a soft router refresh re-reads the page's
// server data without moving the scroll position.
import { useActionState, useEffect, useState } from "react";
import { useRouter } from "next/navigation";

import { decideAnimalPurchaseAction, type AnimalPurchaseDecisionState } from "./animal-purchase-actions";

export type AnimalPurchaseDecisionLabels = {
  title: string;
  note: string;
  noteHint: string;
  accept: string;
  reject: string;
  deciding: string;
  /** Backend sentences by outcome code (`action.<code>`), resolved by the page. */
  outcomes: Record<string, string>;
};

const INITIAL: AnimalPurchaseDecisionState = { status: "idle", code: "", ticket: 0 };
const REFRESH_AFTER_MS = 1400;

export function AnimalPurchaseDecisionForm({
  candidateId,
  rowVersion,
  labels,
}: {
  candidateId: string;
  rowVersion: number;
  labels: AnimalPurchaseDecisionLabels;
}) {
  const router = useRouter();
  const [note, setNote] = useState("");
  const [state, formAction, pending] = useActionState(decideAnimalPurchaseAction, INITIAL);
  const noteId = `ap-note-${candidateId}`;

  useEffect(() => {
    // The server re-reads the row (its decision chip, who decided, the note) in place, once the
    // sentence has been on screen long enough to read; the scroll position does not move.
    if (state.status !== "success") return;
    const timer = window.setTimeout(() => router.refresh(), REFRESH_AFTER_MS);
    return () => window.clearTimeout(timer);
  }, [state.ticket, state.status, router]);

  const message = state.status === "idle" ? "" : labels.outcomes[state.code] || labels.outcomes.error_form || "";
  const decided = state.status === "success";

  return (
    <form action={formAction} className="ap-decision" aria-busy={pending}>
      <input type="hidden" name="candidate_id" value={candidateId} />
      <input type="hidden" name="row_version" value={String(rowVersion)} />
      <div className="ap-decision-row">
        <label htmlFor={noteId} className="sr-only">
          {labels.note}
        </label>
        <input
          id={noteId}
          name="note"
          type="text"
          className="ap-decision-note"
          value={note}
          onChange={(event) => setNote(event.target.value)}
          placeholder={labels.noteHint}
          disabled={pending || decided}
          autoComplete="off"
        />
        <button type="submit" name="decision" value="accept" className="btn primary" disabled={pending || decided}>
          {pending ? labels.deciding : labels.accept}
        </button>
        <button
          type="submit"
          name="decision"
          value="reject"
          className="btn"
          disabled={pending || decided || !note.trim()}
          title={!note.trim() ? labels.note : undefined}
        >
          {labels.reject}
        </button>
      </div>
      {message ? (
        <div className={state.status === "success" ? "ap-decision-msg ok" : "ap-decision-msg bad"} role="status">
          {message}
        </div>
      ) : null}
    </form>
  );
}

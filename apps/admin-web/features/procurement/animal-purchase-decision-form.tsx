"use client";

// The Accept / Reject form for ONE pending animal on /procurement/animal-purchases.
//
// Rendered only for a pending row and only when the backend-declared `decide_animal_purchase`
// control is enabled — the server component decides both; this file never inspects a role. Every
// label arrives as a prop resolved from the page contract's copy map. Reject stays disabled until
// a note is typed (copy `decision.note` says so); Accept is one click. Both post the same Server
// Action with a derived idempotency key and the row_version fence.
import { useState } from "react";
import { useFormStatus } from "react-dom";

import { decideAnimalPurchaseAction } from "./animal-purchase-actions";

export type AnimalPurchaseDecisionLabels = {
  title: string;
  note: string;
  noteHint: string;
  accept: string;
  reject: string;
  deciding: string;
};

export function AnimalPurchaseDecisionForm({
  candidateId,
  rowVersion,
  returnTo,
  labels,
}: {
  candidateId: string;
  rowVersion: number;
  /** The current list URL, carried through the action's redirect so the filter survives. */
  returnTo: string;
  labels: AnimalPurchaseDecisionLabels;
}) {
  const [note, setNote] = useState("");
  const noteId = `ap-note-${candidateId}`;
  return (
    <form action={decideAnimalPurchaseAction} className="ap-decision" style={{ display: "flex", flexDirection: "column", gap: 8 }}>
      <input type="hidden" name="candidate_id" value={candidateId} />
      <input type="hidden" name="row_version" value={String(rowVersion)} />
      <input type="hidden" name="return_to" value={returnTo} />
      <div className="bt">{labels.title}</div>
      <div className="fld" style={{ marginBottom: 0 }}>
        <label htmlFor={noteId}>{labels.note}</label>
        <textarea
          id={noteId}
          name="note"
          rows={2}
          value={note}
          onChange={(event) => setNote(event.target.value)}
          placeholder={labels.noteHint}
        />
      </div>
      <DecisionButtons labels={labels} noteReady={Boolean(note.trim())} />
    </form>
  );
}

function DecisionButtons({ labels, noteReady }: { labels: AnimalPurchaseDecisionLabels; noteReady: boolean }) {
  // useFormStatus reads the enclosing form's pending state so both buttons lock while one
  // submit is in flight — the idempotency key already makes a double post harmless; this only
  // keeps the screen honest about it.
  const { pending } = useFormStatus();
  return (
    <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "center" }}>
      <button type="submit" name="decision" value="accept" className="btn primary" disabled={pending}>
        {pending ? labels.deciding : labels.accept}
      </button>
      <button
        type="submit"
        name="decision"
        value="reject"
        className="btn"
        disabled={pending || !noteReady}
        title={!noteReady ? labels.note : undefined}
      >
        {labels.reject}
      </button>
    </div>
  );
}

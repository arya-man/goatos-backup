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
import { HiddenField } from "@/components/app/hidden-field";
import { useActionState, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

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
    <Stack component="form" action={formAction} aria-busy={pending} spacing={1}>
      <HiddenField name="candidate_id" value={candidateId} />
      <HiddenField name="row_version" value={String(rowVersion)} />
      {/* Template form row: a multiline outlined note (the hint wraps instead of clipping on a
          phone, FJ3 P1-24) with Accept / Reject beside it, stacked at xs. */}
      <Stack direction={{ xs: "column", sm: "row" }} spacing={1.5} sx={{ alignItems: { sm: "flex-start" } }}>
        <TextField
          id={noteId}
          name="note"
          size="small"
          fullWidth
          multiline
          maxRows={4}
          label={labels.note}
          value={note}
          onChange={(event) => setNote(event.target.value)}
          placeholder={labels.noteHint}
          disabled={pending || decided}
          autoComplete="off"
        />
        <Stack direction="row" spacing={1} sx={{ flexShrink: 0 }}>
          <Button type="submit" name="decision" value="accept" variant="contained" color="primary" disabled={pending || decided}>
            {pending ? labels.deciding : labels.accept}
          </Button>
          <Button
            type="submit"
            name="decision"
            value="reject"
            variant="outlined"
            color="error"
            disabled={pending || decided || !note.trim()}
            title={!note.trim() ? labels.note : undefined}
          >
            {labels.reject}
          </Button>
        </Stack>
      </Stack>
      {message ? (
        <Typography role="status" variant="caption" sx={{ color: state.status === "success" ? "success.main" : "error.main" }}>
          {message}
        </Typography>
      ) : null}
    </Stack>
  );
}

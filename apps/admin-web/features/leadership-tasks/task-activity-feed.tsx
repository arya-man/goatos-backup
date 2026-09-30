"use client";

import { Iconify } from "@/components/minimal/iconify";
import { useId, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LeadershipTaskActivity } from "@/lib/api/server";

import { segmentNoteBody } from "./note-mentions";
import { statusTone } from "./task-presentation";
import type { TaskRow } from "./task-row";

/**
 * The task's Activity, shaped like Jira's: three views over ONE backend list.
 *
 * `activity` is `LeadershipTask.activity` exactly as the backend composed it — newest first,
 * one row per FACT that changed (created, status, title, brief, deadline, comment, cancel), each
 * carrying the actor's name and initials, the farm-clock time, the two ends of the change as
 * words AND as keys, and a one-line sentence. History is every row that is not a comment;
 * Comments is the `commented` rows, whose text lives on the matching note in `notes[]` (a
 * comment is stored once); All is both. Nothing here re-orders, re-dates or re-words a row: the
 * tabs FILTER, the rows RENDER.
 *
 * Client-side only for the selected tab. It is `useState`, not a URL param, because switching a
 * view must not re-run the route Server Component (`AGENTS.md` → same-page UI state).
 *
 * COLOUR is the theme's: a status change draws its two chips from the SAME tone tokens the
 * status control at the top of the panel uses (`statusTone`), keyed on the row's stored status
 * KEY (`from_value` / `to_value`), never parsed from its label.
 */
export type ActivityView = "all" | "history" | "comments";

export const ACTIVITY_VIEWS: readonly ActivityView[] = ["all", "history", "comments"];

/** The id prefix of an activity row the composer has NOT yet had confirmed by the backend. */
export const PENDING_ACTIVITY_PREFIX = "pending:";

export function isCommentEntry(entry: Pick<LeadershipTaskActivity, "kind">): boolean {
  return entry.kind === "commented";
}

/** Which rows a view shows. Exported so the tab logic is unit-testable without React. */
export function activityForView<T extends Pick<LeadershipTaskActivity, "kind">>(
  activity: readonly T[],
  view: ActivityView,
): T[] {
  if (view === "history") return activity.filter((entry) => !isCommentEntry(entry));
  if (view === "comments") return activity.filter((entry) => isCommentEntry(entry));
  return [...activity];
}

export function TaskActivityFeed({
  activity,
  notes,
  pageContract,
  composer,
  older,
  initialView = "all",
  loading = false,
}: {
  activity: readonly LeadershipTaskActivity[];
  notes: TaskRow["notes"];
  pageContract: AdminUiPageContract;
  /** The comment composer, rendered FIRST -- above the feed, under the tabs -- so a reader never
   *  scrolls past every comment to write one (maintainer, 2026-09-18). */
  composer?: React.ReactNode;
  /** The "older" control, rendered after the list when older rows exist. */
  older?: React.ReactNode;
  initialView?: ActivityView;
  /** While the full feed is loading the caller shows "Loading activity…"; no empty line beside it. */
  loading?: boolean;
}) {
  const [view, setView] = useState<ActivityView>(initialView);
  const panelID = useId();
  const rows = activityForView(activity, view);
  const notesByID = new Map(notes.map((note) => [note.note_id, note]));
  const counts = {
    all: activity.length,
    history: activity.filter((entry) => !isCommentEntry(entry)).length,
    comments: activity.filter(isCommentEntry).length,
  };
  const emptyKey =
    view === "comments"
      ? "activity.empty_comments"
      : view === "history"
        ? "activity.empty_history"
        : "activity.empty";
  const emptyFallback =
    view === "comments"
      ? "No comments on this task yet."
      : view === "history"
        ? "No changes recorded on this task yet."
        : "Nothing has happened on this task yet.";

  return (
    <div className="ltd-feed" data-testid="ltd-activity-feed" data-ltd-view={view}>
      <div className="ltd-feed-tabs" role="tablist" aria-label={copy(pageContract, "activity.tabs_aria", "Activity views")}>
        {ACTIVITY_VIEWS.map((candidate) => (
          <button
            key={candidate}
            type="button"
            role="tab"
            aria-selected={candidate === view}
            id={`${panelID}-tab-${candidate}`}
            aria-controls={`${panelID}-panel`}
            className={`ltd-feed-tab${candidate === view ? " on" : ""}`}
            data-ltd-tab={candidate}
            onClick={() => setView(candidate)}
          >
            {copy(pageContract, `activity.tab_${candidate}`, tabFallback(candidate))}
            <span className="ltd-feed-count" aria-hidden="true">
              {counts[candidate]}
            </span>
          </button>
        ))}
      </div>

      {composer}

      {rows.length ? (
        <div role="tabpanel" id={`${panelID}-panel`} aria-labelledby={`${panelID}-tab-${view}`}>
        <ol className="ltd-activity">
          {rows.map((entry) => {
            // A comment still travelling to the server (`task-activity-composer.tsx` mints its
            // provisional id with this prefix) is dimmed, and wears the composer's own "me"
            // avatar, until the backend's row -- real actor, real initials -- replaces it.
            const pending = entry.id.startsWith(PENDING_ACTIVITY_PREFIX);
            return (
            <li key={entry.id} className="ltd-act" data-ltd-kind={entry.kind} data-ltd-pending={pending ? "true" : undefined}>
              <span className={`ltd-av ltd-av-sm${pending ? " ltd-av-me" : ""}`} aria-hidden="true">
                {pending ? <Iconify icon="solar:chat-round-dots-bold" /> : entry.actor_initials || "·"}
              </span>
              <div className="ltd-act-tx">
                <div className="ltd-act-line">
                  <b>{entry.actor_name || copy(pageContract, "feed.update", "Task update")}</b>
                  <span className="ltd-act-verb">
                    {copy(pageContract, `activity.${entry.kind}`, verbFallback(entry.kind))}
                  </span>
                  <Change entry={entry} />
                </div>
                {isCommentEntry(entry) ? (
                  <NoteBody note={notesByID.get(entry.note_id)} fallback={entry.summary} />
                ) : null}
                <time className="ltd-act-when" dateTime={entry.occurred_at}>
                  {entry.occurred_label}
                </time>
              </div>
            </li>
            );
          })}
        </ol>
        </div>
      ) : loading ? null : (
        <p className="ltd-quiet" role="tabpanel" id={`${panelID}-panel`} aria-labelledby={`${panelID}-tab-${view}`}>
          {copy(pageContract, emptyKey, emptyFallback)}
        </p>
      )}

      {older}
    </div>
  );
}

/**
 * A comment's text, with each person it NAMED drawn as a brand-toned chip (`.ltd-mention`).
 * The chips come from the note's stored `mentions` (`note-mentions.ts`), so "@Manju" reads as a
 * mention only when Manju was actually picked and stored -- never from a regex over the prose.
 */
function NoteBody({
  note,
  fallback,
}: {
  note: TaskRow["notes"][number] | undefined;
  fallback: string;
}) {
  if (!note) return <p>{fallback}</p>;
  const segments = segmentNoteBody(note.body, note.mentions ?? []);
  return (
    <p className="ltd-note">
      {segments.map((segment, index) =>
        segment.kind === "mention" ? (
          <span key={index} className="ltd-mention" data-mention-user-id={segment.user_id}>
            {segment.text}
          </span>
        ) : (
          <span key={index}>{segment.text}</span>
        ),
      )}
    </p>
  );
}

/**
 * The "from → to" half of a history line. A status change draws the two status chips with an
 * arrow, as Jira does; a title or deadline change reads the two labels in plain text; kinds
 * with no ends (created, commented, cancelled) draw nothing here.
 */
function Change({ entry }: { entry: LeadershipTaskActivity }) {
  if (entry.kind === "status_changed" || entry.kind === "cancelled") {
    return (
      <span className="ltd-act-change">
        <StatusChip value={entry.from_value} label={entry.from_label} />
        <span className="ltd-act-arrow" aria-hidden="true">
          →
        </span>
        <StatusChip value={entry.to_value} label={entry.to_label} />
      </span>
    );
  }
  if (!entry.from_label && !entry.to_label) return null;
  if (entry.kind === "brief_changed") return null;
  return (
    <span className="ltd-act-change">
      <span className="ltd-act-val">{entry.from_label || "—"}</span>
      <span className="ltd-act-arrow" aria-hidden="true">
        →
      </span>
      <span className="ltd-act-val">{entry.to_label || "—"}</span>
    </span>
  );
}

function StatusChip({ value, label }: { value: string; label: string }) {
  if (!label) return <span className="ltd-act-val">—</span>;
  return (
    <span className={`ltd-actchip ltd-status-${statusTone(value as TaskRow["status"])}`}>{label}</span>
  );
}

function tabFallback(view: ActivityView): string {
  if (view === "history") return "History";
  if (view === "comments") return "Comments";
  return "All";
}

function verbFallback(kind: string): string {
  switch (kind) {
    case "created":
      return "created the task";
    case "status_changed":
      return "changed the status";
    case "assignee_changed":
      return "changed the assignee";
    case "deadline_changed":
      return "changed the deadline";
    case "title_changed":
      return "changed the title";
    case "brief_changed":
      return "edited the brief";
    case "commented":
      return "commented";
    case "cancelled":
      return "cancelled the task";
  }
  return "updated the task";
}

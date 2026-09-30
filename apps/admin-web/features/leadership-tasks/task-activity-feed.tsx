"use client";

import { useId, useState } from "react";

import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Tab from "@mui/material/Tab";
import Tabs from "@mui/material/Tabs";
import Typography from "@mui/material/Typography";
import { varAlpha } from "minimal-shared/utils";

import { Iconify } from "@/components/minimal/iconify";
import { Label, type LabelColor } from "@/components/minimal/label";
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
 *
 * ANATOMY is the template kanban details drawer (sections/kanban/details): MUI Tabs with a Label
 * count per tab (`kanban-details.tsx` + the order list tabs), then the comment list rows
 * (`kanban-details-comment-list.tsx`: Avatar, `subtitle2` name, `caption` time, `body2` text).
 * `data-ltd-*` attributes are the test hooks; no stylesheet class is involved.
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
    <Box data-testid="ltd-activity-feed" data-ltd-view={view} sx={{ minWidth: 0 }}>
      <Tabs
        value={view}
        onChange={(_, next: ActivityView) => setView(next)}
        aria-label={copy(pageContract, "activity.tabs_aria", "Activity views")}
        data-ltd-tabs="true"
        sx={{ mb: 2.5, boxShadow: (theme) => `inset 0 -2px 0 0 ${varAlpha(theme.vars.palette.grey["500Channel"], 0.08)}` }}
      >
        {ACTIVITY_VIEWS.map((candidate) => (
          <Tab
            key={candidate}
            value={candidate}
            id={`${panelID}-tab-${candidate}`}
            aria-controls={`${panelID}-panel`}
            data-ltd-tab={candidate}
            iconPosition="end"
            label={copy(pageContract, `activity.tab_${candidate}`, tabFallback(candidate))}
            icon={
              <Label variant={candidate === view ? "filled" : "soft"} color="default" aria-hidden="true" data-ltd-count={counts[candidate]}>
                {counts[candidate]}
              </Label>
            }
          />
        ))}
      </Tabs>

      {composer}

      {rows.length ? (
        <Box role="tabpanel" id={`${panelID}-panel`} aria-labelledby={`${panelID}-tab-${view}`}>
          <Box component="ol" data-ltd-list="true" sx={{ listStyle: "none", m: 0, mb: 2, p: 0, display: "flex", flexDirection: "column", gap: 2.5 }}>
            {rows.map((entry) => {
              // A comment still travelling to the server (`task-activity-composer.tsx` mints its
              // provisional id with this prefix) is dimmed, and wears the composer's own "me"
              // avatar, until the backend's row -- real actor, real initials -- replaces it.
              const pending = entry.id.startsWith(PENDING_ACTIVITY_PREFIX);
              return (
                <Box
                  component="li"
                  key={entry.id}
                  data-ltd-kind={entry.kind}
                  data-ltd-pending={pending ? "true" : undefined}
                  sx={{ display: "flex", gap: 2, minWidth: 0, "&[data-ltd-pending='true']": { opacity: 0.62 } }}
                >
                  <ActivityAvatar pending={pending} initials={entry.actor_initials} />
                  <Box sx={{ minWidth: 0, flex: 1, display: "flex", flexDirection: "column", gap: 0.5 }}>
                    <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", columnGap: 0.75, rowGap: 0.5, typography: "body2" }}>
                      <Typography component="b" variant="subtitle2" data-ltd-actor="true">
                        {entry.actor_name || copy(pageContract, "feed.update", "Task update")}
                      </Typography>
                      <Box component="span" data-ltd-verb="true" sx={{ color: "text.secondary" }}>
                        {copy(pageContract, `activity.${entry.kind}`, verbFallback(entry.kind))}
                      </Box>
                      <Change entry={entry} />
                    </Box>
                    {isCommentEntry(entry) ? (
                      <NoteBody note={notesByID.get(entry.note_id)} fallback={entry.summary} />
                    ) : null}
                    <Typography
                      component="time"
                      variant="caption"
                      dateTime={entry.occurred_at}
                      data-ltd-when="true"
                      sx={{ color: "text.disabled", fontVariantNumeric: "tabular-nums", fontStyle: pending ? "italic" : undefined }}
                    >
                      {entry.occurred_label}
                    </Typography>
                  </Box>
                </Box>
              );
            })}
          </Box>
        </Box>
      ) : loading ? null : (
        <Typography
          variant="body2"
          role="tabpanel"
          id={`${panelID}-panel`}
          aria-labelledby={`${panelID}-tab-${view}`}
          data-ltd-empty="true"
          sx={{ color: "text.secondary", overflowWrap: "anywhere" }}
        >
          {copy(pageContract, emptyKey, emptyFallback)}
        </Typography>
      )}

      {older}
    </Box>
  );
}

/**
 * The row's avatar: the backend's initials, or -- for the reader's own comment still in flight --
 * the composer's "me" mark on a primary tint.
 */
export function ActivityAvatar({ pending = false, initials }: { pending?: boolean; initials?: string }) {
  return (
    <Avatar
      aria-hidden="true"
      data-ltd-avatar={pending ? "me" : "actor"}
      sx={(theme) => ({
        width: "var(--sp-4)",
        height: "var(--sp-4)",
        flex: "none",
        typography: "caption",
        fontWeight: "fontWeightBold",
        ...(pending
          ? { color: "primary.main", bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.16) }
          : {}),
      })}
    >
      {pending ? <Iconify icon="solar:chat-round-dots-bold" width={16} /> : initials || "·"}
    </Avatar>
  );
}

/**
 * A comment's text, with each person it NAMED drawn as a brand-toned chip.
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
  const body = { m: 0, whiteSpace: "pre-wrap", overflowWrap: "anywhere" } as const;
  if (!note) return <Typography variant="body2" sx={body}>{fallback}</Typography>;
  const segments = segmentNoteBody(note.body, note.mentions ?? []);
  return (
    <Typography variant="body2" data-ltd-note="true" sx={body}>
      {segments.map((segment, index) =>
        segment.kind === "mention" ? (
          <Box
            component="span"
            key={index}
            data-mention-user-id={segment.user_id}
            sx={(theme) => ({
              px: 0.625,
              py: 0.125,
              borderRadius: 0.75,
              fontWeight: "fontWeightSemiBold",
              whiteSpace: "nowrap",
              color: "primary.main",
              bgcolor: varAlpha(theme.vars.palette.primary.mainChannel, 0.12),
            })}
          >
            {segment.text}
          </Box>
        ) : (
          <span key={index}>{segment.text}</span>
        ),
      )}
    </Typography>
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
      <ChangeRow>
        <StatusChip value={entry.from_value} label={entry.from_label} />
        <Arrow />
        <StatusChip value={entry.to_value} label={entry.to_label} />
      </ChangeRow>
    );
  }
  if (!entry.from_label && !entry.to_label) return null;
  if (entry.kind === "brief_changed") return null;
  return (
    <ChangeRow>
      <ChangeValue>{entry.from_label || "—"}</ChangeValue>
      <Arrow />
      <ChangeValue>{entry.to_label || "—"}</ChangeValue>
    </ChangeRow>
  );
}

function ChangeRow({ children }: { children: React.ReactNode }) {
  return (
    <Box component="span" data-ltd-change="true" sx={{ display: "inline-flex", flexWrap: "wrap", alignItems: "center", gap: 0.75, minWidth: 0 }}>
      {children}
    </Box>
  );
}

function ChangeValue({ children }: { children: React.ReactNode }) {
  return (
    <Box component="span" data-ltd-value="true" sx={{ fontWeight: "fontWeightSemiBold", color: "text.primary", overflowWrap: "anywhere" }}>
      {children}
    </Box>
  );
}

function Arrow() {
  return (
    <Box component="span" aria-hidden="true" data-ltd-arrow="true" sx={{ color: "text.secondary", fontWeight: "fontWeightBold" }}>
      →
    </Box>
  );
}

const TONE_COLOR: Partial<Record<ReturnType<typeof statusTone>, LabelColor>> = {
  warn: "warning",
  info: "info",
  ok: "success",
  mut: "default",
};

function StatusChip({ value, label }: { value: string; label: string }) {
  if (!label) return <ChangeValue>—</ChangeValue>;
  const tone = statusTone(value as TaskRow["status"]);
  return (
    <Label variant="soft" color={TONE_COLOR[tone] ?? "default"} data-ltd-chip={tone}>
      {label}
    </Label>
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

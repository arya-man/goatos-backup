import type { LeadershipTaskAttachment } from "@/lib/api/server";
import type { Tone } from "@/components/ui-primitives";

export type TaskStatus = "open" | "doing" | "done";

export type TaskRow = {
  id: string;
  number: string;
  title: string;
  body: string;
  comment: string;
  canComment: boolean;
  rowVersion: number;
  status: TaskStatus;
  statusOptions: Array<{ key: string; label: string }>;
  assignee: string;
  assigneeRole: string;
  raisedBy: string;
  age: string;
  attachments: number;
  evidence: string;
  attachmentKinds: string[];
  attachmentRows: LeadershipTaskAttachment[];
  notes: Array<{
    note_id: string;
    author_name: string;
    body: string;
    created_at: string;
  }>;
  /**
   * The deadline countdown, backend-composed and rendered verbatim (maintainer decision
   * 2026-09-14): days left to the deadline (0 = due today, negative = overdue), its tone
   * (ok = green; near and over = red), the deadline itself and the sentence beneath. A task
   * without a deadline carries none of these and shows no counter.
   */
  daysLeft: number | null;
  daysLeftLabel: string;
  deadlineTone: "" | "ok" | "near" | "over";
  deadlineLabel: string;
  deadlineStateLabel: string;
};

export type ScopeRow = {
  key: string;
  label: string;
  count: number;
  detail: string;
  tone: Tone;
  selected: boolean;
  emptyMessage?: string;
};

export type FeedTone = "f-info" | "f-pur" | "f-warn" | "f-ok";


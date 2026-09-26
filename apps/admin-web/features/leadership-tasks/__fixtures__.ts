/**
 * PREVIEW-ONLY fixtures.
 *
 * These invented people, tasks and counts used to live inside `leadership-tasks-page.tsx`, which
 * meant the production bundle carried fabricated rows one prop away from being rendered. They now
 * live here and are imported ONLY by `app/tasks-preview/page.tsx`, which passes them in.
 */
import type { TaskRow, ScopeRow } from "./types";

export const fixtureTasks: TaskRow[] = [
  {
    id: "1",
    number: "#18",
    title: "Check CPT west fence repair before evening close",
    body: "Confirm the west fence patch before close and attach the completion proof.",
    comment: "Park team acknowledged.",
    canComment: true,
    rowVersion: 4,
    status: "doing",
    statusOptions: [{ key: "done", label: "Done" }],
    assignee: "Satish",
    assigneeRole: "Park Head",
    raisedBy: "Manju",
    age: "Today",
    attachments: 3,
    evidence: "video, voice note",
    attachmentKinds: ["video", "audio"],
    attachmentRows: [],
    notes: [
      {
        note_id: "fixture-note-1",
        author_name: "Satish",
        body: "Park team acknowledged.",
        created_at: new Date().toISOString(),
      },
    ],
    daysLeft: 4,
    daysLeftLabel: "4 days left",
    deadlineTone: "ok",
    deadlineLabel: "18/09/2026 17:00",
    deadlineStateLabel: "Due in 4 days",
  },
  {
    id: "2",
    number: "#17",
    title: "Confirm director handoff for feed unloading delay",
    body: "Capture what delayed unloading and who owns the next checkpoint.",
    comment: "Waiting for vendor note.",
    canComment: true,
    rowVersion: 2,
    status: "open",
    statusOptions: [{ key: "in_progress", label: "Doing" }],
    assignee: "Manohar",
    assigneeRole: "Feed Director",
    raisedBy: "Ravi",
    age: "Today",
    attachments: 2,
    evidence: "note, file",
    attachmentKinds: ["file"],
    attachmentRows: [],
    notes: [
      {
        note_id: "fixture-note-2",
        author_name: "Manohar",
        body: "Waiting for vendor note.",
        created_at: new Date().toISOString(),
      },
    ],
    daysLeft: -4,
    daysLeftLabel: "4 days over",
    deadlineTone: "over",
    deadlineLabel: "10/09/2026 12:00",
    deadlineStateLabel: "Overdue by 4 days",
  },
  {
    id: "3",
    number: "#16",
    title: "Send Borewell-2 motor reading after restart",
    body: "Share the post-restart reading with a short clip.",
    comment: "Completed.",
    canComment: false,
    rowVersion: 7,
    status: "done",
    statusOptions: [],
    assignee: "Prakash",
    assigneeRole: "Employee",
    raisedBy: "Manju",
    age: "Yesterday",
    attachments: 4,
    evidence: "completion video",
    attachmentKinds: ["video"],
    attachmentRows: [],
    notes: [
      {
        note_id: "fixture-note-3",
        author_name: "Prakash",
        body: "Completed.",
        created_at: new Date().toISOString(),
      },
    ],
    daysLeft: null,
    daysLeftLabel: "",
    deadlineTone: "",
    deadlineLabel: "",
    deadlineStateLabel: "",
  },
];

export const fixtureScopes: ScopeRow[] = [
  {
    key: "assigned_to_me",
    label: "Assigned to me",
    count: 2,
    detail: "My action queue",
    tone: "info",
    selected: false,
  },
  {
    key: "assigned_by_me",
    label: "Assigned by me",
    count: 7,
    detail: "Follow-ups I raised",
    tone: "warn",
    selected: false,
  },
  {
    key: "team_progress",
    label: "Team progress",
    count: 18,
    detail: "Open team work",
    tone: "ok",
    selected: true,
  },
];


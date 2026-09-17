import type { AdminUiPageContract } from "@/lib/admin-ui-contract";

/**
 * The page contract the FIXTURE host (`app/tasks-preview`) renders against.
 *
 * `route_id` is the live `leadership-tasks` on purpose. The copy below mirrors the backend's
 * `pageSpecificCopy("leadership-tasks")` map, so the preview cannot show wording the real screen
 * does not have — which is the whole point of a fixture host. The table shape mirrors
 * `backend/internal/adminui/app/service.go`'s
 * `table("leadership-task-progress", …)` column for column, so a column set that drifts on the
 * backend shows up as a preview that stops rendering rather than as a silent difference.
 */
export const leadershipTasksFixtureContract: AdminUiPageContract = {
  route_id: "leadership-tasks",
  href: "/tasks-preview",
  path_pattern: "/tasks-preview",
  title: "Tasks",
  subtitle: "Tasks raised across CXOs, directors and park heads, with notes and attachments.",
  surface_kind: "monitoring-screen",
  source_scope: ["local-preview"],
  sections: [],
  tables: [
    {
      id: "leadership-task-progress",
      title: "Team progress",
      data_source: "/app/leadership-tasks",
      columns: [
        { key: "task", label: "Task", sortable: false, visible: true },
        { key: "assignee", label: "Assignee", sortable: false, visible: true },
        { key: "raised_by", label: "Raised By", sortable: false, visible: true },
        { key: "status", label: "Status", sortable: false, visible: true },
        { key: "evidence", label: "Evidence", sortable: false, visible: true },
        { key: "days_left", label: "Days left", sortable: false, visible: true },
      ],
      filters: [
        { key: "search", label: "Search", kind: "search", enabled: true, disabled_reason: "" },
        { key: "filters", label: "Filters", kind: "drawer", enabled: true, disabled_reason: "" },
      ],
      sort_keys: [],
      page_size_options: [5, 10, 25, 50],
      row_click: {
        enabled: true,
        param: "task_id",
        target_drawer: "record",
        summary_fields: ["id", "title", "status", "owner", "next_action"],
        detail_fields: ["contract_object"],
      },
      summary_fields: ["id", "title", "status", "owner", "next_action"],
      detail_fields: ["contract_object"],
    },
  ],
  drawers: [],
  controls: [],
  /**
   * The page's copy, mirroring `pageSpecificCopy("leadership-tasks")` in
   * `backend/internal/adminui/app/service.go` KEY FOR KEY, plus the handful of SHARED base-map
   * keys from the same file that this page's controls read.
   *
   * It is spelled out here because the copy contract moved to the backend and
   * `COPY_FALLBACKS["leadership-tasks"]` in `lib/admin-ui-contract.ts` went with it. The live
   * /tasks page gets this map from the backend; the fixture host has no backend, and `copy()`
   * THROWS on a fixed key it cannot resolve — so with the fallback gone and `copy: {}` here,
   * /tasks-preview stopped rendering at the first key it asked for (`crumb`).
   *
   * Keep it in step with the Go map above by hand: a value that drifts shows the preview wording
   * the real screen does not have, which is the one thing a fixture host must never do.
   */
  copy: {
    "crumb": "Operations",
    "filter.bar_aria": "Filter tasks",
    "filter.search_label": "Search tasks, or type a number",
    "filter.all_option": "All",
    "filter.assignee": "Assignee",
    "filter.assignee_pinned": "This scope is already only your tasks.",
    "filter.raiser": "Raised by",
    "filter.raiser_pinned": "This scope is already only the tasks you raised.",
    "filter.sort": "Sort",
    "filter.deadline_from": "Deadline from",
    "filter.deadline_to": "Deadline to",
    "filter.raised_from": "Raised from",
    "filter.raised_to": "Raised to",
    "filter.range_note": "Pick both ends of a date range — a half range is not applied.",
    "sort.raised_at_desc": "Newest first",
    "sort.raised_at_asc": "Oldest first",
    "sort.deadline_asc": "Deadline soonest",
    "sort.deadline_desc": "Deadline latest",
    "scope.aria": "Task scopes",
    "column.assignee": "Assignee",
    "column.raised_by": "Raised by",
    "column.evidence": "Evidence",
    "table.tasks.noun": "task",
    "label.deadline": "Deadline",
    "label.brief": "Brief",
    "label.assignee_note": "Assignee note",
    "section.selected.title": "Selected task",
    "action.open_task": "Open task",
    "action.edit": "Edit",
    "state.preview": "Preview data",
    "state.can_raise": "Can raise",
    "state.unavailable_tasks": "Tasks could not be loaded. Try again.",
    "empty.tasks": "No tasks match these filters.",
    "empty.tasks_detail": "This queue is clear for the current role and park scope. When work is raised, it will appear here with the owner, evidence, and next status action.",
    "empty.selected": "Select a task",
    "empty.selected_detail": "Choose a row to view its brief, status actions, attachments, and activity updates.",
    "note.label": "Activity update",
    "note.placeholder": "Write the latest status or reply.",
    "note.send": "Send update",
    "feed.update": "Task update",
    "feed.audio": "Audio attached",
    "feed.video": "Video attached",
    "feed.files": "Files attached",
    "picker.voice": "Voice note",
    "picker.media": "Photo or video",
    "picker.file": "File",
    "edit.title": "Edit task",
    "edit.title_field": "Title",
    "edit.body_field": "Brief",
    "edit.deadline_field": "Deadline",
    "edit.deadline_hint": "Date and time the task is due, farm clock (IST). Leave it as it is to keep the stored deadline.",
    "edit.attachments": "Attachments",
    "edit.too_many": "A task carries at most 12 attachments.",
    "edit.save": "Save changes",
    "feedback.task_raised": "Task raised.",
    "feedback.task_updated": "Task status updated.",
    "feedback.task_edited": "Task saved.",
    "feedback.note_added": "Update added to the task.",
    "feedback.version_conflict": "Someone changed this task while you were editing it. Reload the page and try again.",
    "feedback.task_closed": "This task is already finished, so it can no longer be edited.",
    "feedback.forbidden": "Only the person who raised this task can edit it.",
    "feedback.missing_title": "A task needs a title.",
    "feedback.missing_assignee": "Choose who the task is for.",
    "feedback.missing_deadline": "A task needs a deadline.",
    "feedback.missing_note": "Write the update before sending it.",
    "feedback.invalid_deadline": "That deadline is not a date and time.",
    "feedback.invalid_edit": "The task could not be saved. Reload the page and try again.",
    "feedback.invalid_status_change": "That status change could not be applied.",
    "feedback.invalid_idempotency_key": "The form expired. Reload the page and try again.",
    "feedback.too_many_attachments": "A task carries at most 12 attachments.",
    // Shared base map (same Go file), read by this page's chips, banner and sheet.
    "action.apply": "Apply",
    "action.clear": "Clear",
    "action.close": "Close",
    "action.failed_title": "Action failed",
    "action.filters": "Filters",
    "action.success_tag": "done",
    "filter.close_label": "Close filters",
    "pager.matching_rows": "matching rows",
    "pager.rows": "Rows",
    "pager.page": "Page",
    "pager.of": "of",
    "state.loading": "Loading",
    "state.unavailable": "Unavailable",
    "label.placeholder": "—",
  },
  option_groups: [],
  migration_status: "preview",
  validation_notes: [],
};

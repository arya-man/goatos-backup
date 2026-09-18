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
    "filter.search_label": "Search tasks…",
    "filter.search_hint": "Search by title, or type a task number",
    "filter.range_any": "any",
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
    // ---- Status board (`t_view=board`). These keys are NOT in the backend's
    // `pageSpecificCopy("leadership-tasks")` map yet; every read of them uses the 3-arg
    // `copy(contract, key, fallback)` form with the SAME wording, so the live screen renders
    // correctly before the backend ships them and switches to the contract the moment it does.
    // The exact list the backend owner must add is in the handoff for this change.
    "board.view.aria": "Task view",
    "board.view.board": "Board",
    "board.view.list": "List",
    "board.aria": "Tasks by status",
    "board.column.open": "To do",
    "board.column.in_progress": "In progress",
    "board.column.done": "Done",
    "board.column.cancelled": "Cancelled",
    "board.column_empty": "Nothing in this status on this page.",
    "board.on_this_page": "on this page",
    "board.focus_status": "See every task in this status",
    "board.focused_note": "This board is showing one status only, so you can page through all of it.",
    "board.overdue_note": "This board is showing only tasks past their deadline, so you can page through all of them.",
    "board.all_statuses": "Back to all statuses",
    "board.total_unavailable": "The whole-list total for this status is not published.",
    "board.drag_hint": "Drag a card onto a status it is allowed to move to, or open a task and use its status buttons.",
    "board.drag_moving": "Moving",
    "board.partial_note": "Each column shows the tasks on this page of the list. The number beside a status is its true total across the whole list; open one status to page through all of it.",
    "board.people_aria": "Filter by person",
    "board.people_all": "Everyone",
    "column.assignee": "Assignee",
    "column.raised_by": "Raised by",
    "column.evidence": "Evidence",
    "table.tasks.noun": "task",
    "label.deadline": "Deadline",
    "label.brief": "Brief",
    "label.assignee_note": "Assignee note",
    "section.selected.title": "Selected task",
    // Jira-shaped detail panel (features/leadership-tasks/task-detail-panel.tsx). Each of these
    // is read through the 3-arg `copy(contract, key, fallback)` form, so the live screen renders
    // the fallback until the backend's `pageSpecificCopy("leadership-tasks")` map gains the key
    // rather than blanking the shell on a throw.
    "section.details": "Details",
    "section.activity": "Activity",
    "label.status": "Status",
    "status.menu_aria": "Change status",
    "status.cancel_confirm": "Cancel this task? It cannot be reopened afterwards.",
    "label.raised_on": "Raised on",
    "empty.description": "No brief was written for this task.",
    "empty.attachments": "No attachments on this task.",
    "empty.activity": "No updates on this task yet.",
    "empty.selected_raise": "Or raise a new task from the button above the list.",
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
    "note.sending": "Sending…",
    "note.just_now": "just now",
    "note.you": "You",
    "note.failed": "The update could not be sent. Try again.",
    "feed.video": "Video attached",
    "feed.files": "Files attached",
    "picker.voice": "Voice note",
    "picker.media": "Photo or video",
    "picker.file": "File",
    "deadline.day": "Choose a day",
    "deadline.hour": "Hour",
    "deadline.minute": "Minute",
    "deadline.day_min": "Pick a day on or after {date}.",
    "date.previous_month": "Previous month",
    "date.next_month": "Next month",
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
    // One key per refusal, most specific first: `.status` when the task's current chip
    // resolved, `.named` when the person whose move it is resolved, then the plain sentence.
    // There is deliberately no `feedback.version_conflict.named` — a name never resolves on a
    // conflict, because nothing reports WHO made the last status change.
    "feedback.version_conflict.status": "This task was moved to {status} while this board was open, so your move was not applied. The board now shows that — move it from what you see now.",
    "feedback.version_conflict": "This task was changed while this board was open, so your move was not applied. The board now shows the current version.",
    "feedback.task_closed.status": "This task is {status}, and a closed task cannot be moved.",
    "feedback.task_closed": "This task is closed, so its status cannot be changed.",
    "feedback.not_assignee.named": "Only {name}, the person this task is assigned to, can move it.",
    "feedback.not_assignee": "Only the person this task is assigned to can move it.",
    "feedback.not_raiser.named": "Only {name}, who raised this task, can cancel it.",
    "feedback.not_raiser": "Only the person who raised this task can cancel it.",
    "feedback.invalid_status_transition.status": "This task is {status}, and that is not a move it can make from there.",
    "feedback.invalid_status_transition": "That is not a move this task can make from where it is now.",
    "feedback.mention_not_visible": "A person can only be named on a task they can already see. Pick from the list that appears when you type @, or share the task with them first.",
    "feedback.invalid_mention": "A name that is not on this farm's leadership roster cannot be named on a task. Pick the person from the list that appears when you type @.",
    "feedback.too_many_mentions": "One update can name at most 20 people.",
    "feedback.mention_without_note": "Write the update before naming anyone in it.",
    "feedback.forbidden": "Only the person who raised this task can edit it.",
    "feedback.missing_title": "A task needs a title.",
    "feedback.missing_assignee": "Choose who the task is for.",
    "feedback.missing_deadline": "A task needs a deadline.",
    "feedback.missing_note": "Write the update before sending it.",
    "feedback.invalid_deadline": "That deadline is not a date and time.",
    "feedback.invalid_edit": "The task could not be saved. Reload the page and try again.",
    "feedback.invalid_status_change": "That move did not say which task to move, or where to move it to. Reload the board and drag the card again.",
    "feedback.invalid_idempotency_key": "The form expired. Reload the page and try again.",
    "feedback.too_many_attachments": "A task carries at most 12 attachments.",
    // The activity feed (task-activity-feed.tsx): three views over the backend's
    // `LeadershipTask.activity`. Mirrors the ACTIVITY block of pageSpecificCopy("leadership-tasks").
    "activity.tabs_aria": "Activity views",
    "activity.tab_all": "All",
    "activity.tab_history": "History",
    "activity.tab_comments": "Comments",
    "activity.created": "created the task",
    "activity.status_changed": "changed the status",
    "activity.assignee_changed": "changed the assignee",
    "activity.deadline_changed": "changed the deadline",
    "activity.title_changed": "changed the title",
    "activity.brief_changed": "edited the brief",
    "activity.commented": "commented",
    "activity.cancelled": "cancelled the task",
    "activity.updated": "updated the task",
    "activity.empty": "Nothing has happened on this task yet.",
    "activity.empty_history": "No changes recorded on this task yet.",
    "activity.empty_comments": "No comments on this task yet.",
    // Shared base map (same Go file), read by this page's chips, banner and sheet -- and by the
    // SHARED `components/worklist-pager.tsx` this page renders, which is where `action.previous`,
    // `action.next`, `pager.page` and `pager.rows` are read from.
    "action.apply": "Apply",
    "action.previous": "Previous",
    "action.next": "Next",
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

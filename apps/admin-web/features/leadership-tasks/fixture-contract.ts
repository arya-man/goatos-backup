import type { AdminUiPageContract } from "@/lib/admin-ui-contract";

/**
 * The page contract the FIXTURE host (`app/tasks-preview`) renders against.
 *
 * `route_id` is the live `leadership-tasks` on purpose: the copy comes from the same
 * `COPY_FALLBACKS` entry the live page uses, so the preview cannot show wording the real screen
 * does not have — which is the whole point of a fixture host. Only the table shape is declared
 * here, and it mirrors `backend/internal/adminui/app/service.go`'s
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
        { key: "priority", label: "Priority", sortable: false, visible: true },
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
  copy: {},
  option_groups: [],
  migration_status: "preview",
  validation_notes: [],
};

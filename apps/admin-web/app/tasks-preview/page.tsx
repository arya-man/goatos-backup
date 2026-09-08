import { LeadershipTasksPage } from "@/features/leadership-tasks/leadership-tasks-page";
import { MeshaShell } from "@/components/mesha-shell";
import type { AdminWebBootstrapResponse } from "@/lib/api/server";
import type { Park } from "@/lib/scope";

const previewParks: Park[] = [
  { id: "cpt", code: "CPT", name: "Coimbatore" },
  { id: "chk", code: "CHK", name: "Channapatna" },
];

const navItem = (id: string, label: string, href: string, icon: string, domain = "operations") => ({
  id,
  label,
  href,
  icon,
  badge_key: "",
  enabled: true,
  disabled_reason: "",
  domain,
  extra: {},
});

const previewContract: AdminWebBootstrapResponse = {
  source: "api",
  schema_version: "preview",
  contract_revision: "leadership-tasks-preview",
  family_hashes: {},
  cache_policy: {
    etag: "leadership-tasks-preview",
    in_process_ttl_sec: 0,
    redis_ttl_hint_sec: 0,
    revision_source: "local-preview",
  },
  navigation: {
    primary: [
      navItem("home", "Dashboard", "/", "tower-control"),
      navItem("tasks", "Tasks", "/tasks-preview", "clipboard-check"),
      navItem("approvals", "Approvals", "/approvals", "gavel"),
    ],
    groups: [
      {
        id: "operations",
        label: "Operations",
        icon: "workflow",
        default_open: true,
        badge_key: "",
        leaves: [
          navItem("workflows", "Workflows", "/workflows", "workflow"),
          navItem("dlq", "DLQ", "/operations/dlq", "zap"),
          navItem("audit", "Audit", "/operations/audit", "tower-control"),
        ],
      },
      {
        id: "farm",
        label: "Farm",
        icon: "wheat",
        default_open: true,
        badge_key: "",
        leaves: [
          navItem("feed", "Feed", "/feed", "wheat", "feed"),
          navItem("weighing", "Weighing", "/weighing/weights", "scale", "weighing"),
          navItem("health", "Health", "/health", "stethoscope", "health"),
        ],
      },
    ],
    footer: "Local preview contract",
  },
  nav_chrome: "expanded",
  route_labels: [
    { pattern: "/tasks-preview", label: "Tasks", match: "exact" },
    { pattern: "/tasks", label: "Tasks", match: "exact" },
  ],
  top_bar: {
    product_name: "Mesha OS",
    logo_text: "M",
    scope_mode_toggle: [{ key: "park", label: "Park", title: "Park scope", enabled: true, disabled_reason: "" }],
    park_selector: {
      label: "Park scope",
      enabled: true,
      disabled_reason: "",
      hint: "Preview scope selector",
      options: previewParks.map((park) => ({ key: park.id, label: park.code ?? park.name, title: park.name, enabled: true, disabled_reason: "" })),
    },
    date_range_selector: { label: "Date range", enabled: false, disabled_reason: "Preview", hint: "", options: [] },
    notifications: { label: "Notifications", enabled: false, disabled_reason: "Notifications disabled in preview", hint: "", options: [] },
    role_preview: { display_name: "Ravi", initials: "RA", subtitle: "CEO" },
  },
  role_lenses: [],
  pages: [
    {
      route_id: "tasks-preview",
      href: "/tasks-preview",
      path_pattern: "/tasks-preview",
      title: "Tasks",
      subtitle: "Manual leadership tasks",
      surface_kind: "page",
      source_scope: ["local-preview"],
      sections: [],
      tables: [],
      drawers: [],
      controls: [],
      copy: {},
      option_groups: [],
      migration_status: "preview",
      validation_notes: [],
    },
  ],
  copy: {
    "account.open_menu": "Open account menu",
    "ceo_ai.checking": "Checking",
    "ceo_ai.close": "Close assistant",
    "ceo_ai.hello": "Ask about farm operations.",
    "ceo_ai.hello_meta": "Local preview",
    "ceo_ai.mode_fallback": "Preview mode",
    "ceo_ai.no_answer": "No answer available in preview.",
    "ceo_ai.open": "Open assistant",
    "ceo_ai.placeholder": "Ask CEO AI",
    "ceo_ai.send": "Send",
    "ceo_ai.source_fallback": "Preview",
    "ceo_ai.starter_counts": "Show counts",
    "ceo_ai.starter_due": "What is due?",
    "ceo_ai.starter_help": "Help me inspect",
    "ceo_ai.starter_overdue": "What is overdue?",
    "ceo_ai.subtitle": "Preview assistant",
    "ceo_ai.title": "CEO AI",
    "ceo_ai.unavailable": "Assistant unavailable",
    "ceo_ai.unavailable_meta": "Preview has no backend connection",
    "nav.back": "Back",
    "nav.back_to_prefix": "Back to",
    "nav.collapse": "Collapse navigation",
    "nav.expand": "Expand navigation",
    "route.unavailable": "Unavailable",
    "scope.all_parks": "All parks",
    "scope.all_sheds": "all sheds",
    "scope.company_wide": "company wide",
    "scope.no_parks_for_tenant": "No parks available",
    "scope.park_menu_aria": "Park scope",
    "scope.selected_park": "Selected park",
    "theme.switch_to_dark": "Switch to dark",
    "theme.switch_to_light": "Switch to light",
  },
  display_rules: [],
};

export default function TasksPreviewRoute() {
  return (
    <MeshaShell parks={previewParks} contract={previewContract}>
      <LeadershipTasksPage preview />
    </MeshaShell>
  );
}

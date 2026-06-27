// Package app builds the admin-web UI contract.
package app

import "github.com/vgoats/goatos/backend/internal/adminui/domain"

type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (s *Service) Bootstrap() domain.BootstrapResponse {
	return domain.BootstrapResponse{
		Source:        "api",
		SchemaVersion: "admin-web-ui-v1",
		Navigation:    navigation(),
		RouteLabels:   routeLabels(),
		TopBar:        topBar(),
		RoleLenses:    roleLenses(),
		Pages:         pages(),
		DisplayRules:  displayRules(),
	}
}

func navItem(id, label, href, icon, badgeKey string) domain.NavigationItem {
	return domain.NavigationItem{ID: id, Label: label, Href: href, Icon: icon, BadgeKey: badgeKey, Enabled: true, Extra: map[string]string{}}
}

func navLeaf(id, label, href string, extra map[string]string) domain.NavigationItem {
	if extra == nil {
		extra = map[string]string{}
	}
	return domain.NavigationItem{ID: id, Label: label, Href: href, Enabled: true, Extra: extra}
}

func navigation() domain.NavigationContract {
	return domain.NavigationContract{
		Primary: []domain.NavigationItem{
			navItem("control-tower", "Control Tower", "/", "tower-control", ""),
			navItem("action-center", "Action Center", "/action-center", "zap", "action_center_open_work"),
			navItem("calendar", "Calendar", "/calendar", "calendar-days", ""),
			navItem("protocol-adherence", "Protocol Adherence", "/protocol-adherence", "clipboard-check", ""),
			navItem("workflows", "Workflows", "/workflows", "workflow", ""),
		},
		Groups: []domain.NavigationGroup{
			{
				ID: "phc", Label: "PHC", Icon: "heart-pulse", DefaultOpen: true, BadgeKey: "phc_open_work",
				Leaves: []domain.NavigationItem{navLeaf("phc-vaccination", "Vaccination", "/vaccination", nil)},
			},
			{
				ID: "procurement", Label: "Procurement", Icon: "truck", DefaultOpen: false,
				Leaves: []domain.NavigationItem{navLeaf("procurement-source-entry", "Source Entry", "/procurement/source-entry", nil)},
			},
			{
				ID: "counts", Label: "Counts", Icon: "bar-chart-3", DefaultOpen: false,
				Leaves: []domain.NavigationItem{navLeaf("counts-herd", "Herd Register", "/counts/herd", nil)},
			},
			{
				ID: "admin-data", Label: "Admin / Data Ops", Icon: "edit-3", DefaultOpen: true,
				Leaves: []domain.NavigationItem{
					navLeaf("config", "Config", "/config", map[string]string{"category": "vaccination"}),
					navLeaf("audit-log", "Audit Log", "/operations/audit", nil),
					navLeaf("sop-library", "SOP Library", "/sops", nil),
				},
			},
		},
		Footer: "Mesha · goat operating system",
	}
}

func routeLabels() []domain.RouteLabelRule {
	return []domain.RouteLabelRule{
		{Pattern: "/", Label: "Control Tower", Match: "exact"},
		{Pattern: "/action-center", Label: "Action Center", Match: "exact"},
		{Pattern: "/calendar", Label: "Calendar", Match: "exact"},
		{Pattern: "/protocol-adherence", Label: "Protocol Adherence", Match: "exact"},
		{Pattern: "/workflows/{row_id}", Label: "Workflow record", Match: "pattern"},
		{Pattern: "/workflows", Label: "Workflows", Match: "exact"},
		{Pattern: "/vaccination/execution/sheds/{shed_id}", Label: "Vaccination execution", Match: "pattern"},
		{Pattern: "/vaccination", Label: "Vaccination", Match: "exact"},
		{Pattern: "/procurement/source-entry/loads/{load_id}", Label: "Source load", Match: "pattern"},
		{Pattern: "/procurement/source-entry", Label: "Source Entry", Match: "exact"},
		{Pattern: "/counts/herd", Label: "Herd Register", Match: "exact"},
		{Pattern: "/operations/audit", Label: "Audit Log", Match: "exact"},
		{Pattern: "/config", Label: "Config — Protocol Rules", Match: "exact"},
		{Pattern: "/sops", Label: "SOP Library", Match: "exact"},
		{Pattern: "/goats/{goat_id}", Label: "Goat Passport", Match: "pattern"},
	}
}

func topBar() domain.TopBarContract {
	return domain.TopBarContract{
		ProductName: "Mesha",
		LogoText:    "मे",
		ScopeModeToggle: []domain.TopBarOption{
			{Key: "company", Label: "Company-wide", Title: "Company-wide rollup across all in-scope parks", Enabled: true},
			{Key: "park", Label: "Park-wise", Title: "Park-wise scope", Enabled: true},
		},
		ParkSelector: domain.TopBarControl{
			Label: "Park scope", Enabled: true,
			Hint:    "Shed scope: all sheds — per-shed filtering is intentionally not wired in this slice yet.",
			Options: []domain.TopBarOption{},
		},
		DateRangeSelector: domain.TopBarControl{
			Label: "Date range", Enabled: true,
			Hint: "Point-in-time as-of date is live. Range chips are shown for mock parity but disabled until range semantics are backend-owned.",
			Options: []domain.TopBarOption{
				{Key: "last_7_days", Label: "Last 7 days", Enabled: false, DisabledReason: "Backend range filtering is not defined for the current process-integrity slice."},
				{Key: "last_30_days", Label: "Last 30 days", Enabled: false, DisabledReason: "Backend range filtering is not defined for the current process-integrity slice."},
			},
		},
		Notifications: domain.TopBarControl{Label: "Notifications", Enabled: false, DisabledReason: "Notifications are not wired in this admin-web slice yet.", Options: []domain.TopBarOption{}},
		RolePreview:   domain.RolePreviewActor{DisplayName: "R. Teja", Initials: "RT", Subtitle: "COO · Central Command · all parks"},
	}
}

func roleLenses() []domain.RoleLensContract {
	return []domain.RoleLensContract{
		{ID: "coo", Name: "Superadmin / CEO / COO", AuditShort: "COO", Scope: "all · deep", Description: "Central Command · all parks", Superadmin: true},
		{ID: "health-director", Name: "Health Director", AuditShort: "Health Dir", Scope: "health vertical · all parks", Description: "PHC / health governance view"},
		{ID: "park-head", Name: "Park Head · CBE", AuditShort: "Park Head · CBE", Scope: "all verticals · 1 park", Description: "CBE park leadership view"},
		{ID: "health-manager", Name: "Health Mgr · CBE", AuditShort: "Health Mgr · CBE", Scope: "health vertical · 1 park", Description: "CBE PHC manager view"},
		{ID: "ground", Name: "Assist / Ground · CBE", AuditShort: "Assist · CBE", Scope: "tasks · 1 park", Description: "field execution queue"},
		{ID: "investor", Name: "Investor", AuditShort: "Investor", Scope: "read-only summary", Description: "summary-only lens"},
	}
}

func pages() []domain.PageContract {
	return []domain.PageContract{
		page("control-tower", "/", "/", "Control Tower", "Process-intact / not-intact leadership view for vaccination gaps.", "command-lens",
			[]domain.TableContract{table("open-gaps", "Open vaccination gaps — gap, severity, owner, next action", "/control-tower/vaccination", []string{"gap", "severity", "detail", "owner", "next_action"}, "ct_row")}),
		page("action-center", "/action-center", "/action-center", "Action Center", "Exact vaccination work and gaps to act on now.", "command-lens",
			[]domain.TableContract{table("work-board", "Vaccination work board", "/vaccination/action-center", []string{"work_state", "owner", "due", "task", "next_action"}, "ac_row")}),
		page("calendar", "/calendar", "/calendar", "Calendar", "Vaccination due work by time, owner lane, park, shed, and date.", "command-lens",
			[]domain.TableContract{table("calendar-events", "Due work", "/calendar/vaccination/events", []string{"due_at", "owner", "title", "status", "severity"}, "cal_event")}),
		page("protocol-adherence", "/protocol-adherence", "/protocol-adherence", "Protocol Adherence", "Expected vs actual vaccination ledger, evidence, owner, and next action.", "command-lens",
			[]domain.TableContract{table("adherence-ledger", "Vaccination", "/vaccination/adherence", []string{"expected", "actual", "gap", "severity", "owner", "next_action", "evidence"}, "adh_row")}),
		page("workflows", "/workflows", "/workflows", "Workflows", "Config → obligation → SOP → proof → verification → completion workflow records.", "command-lens",
			[]domain.TableContract{table("workflow-catalog", "Workflow catalog", "/vaccination/action-center", []string{"workflow", "stage", "owner", "next_action", "status"}, "wf_row")}),
		page("workflow-record", "/workflows/{row_id}", "/workflows/{row_id}", "Workflow drilldown", "One vaccination workflow chain reaction record.", "record-drilldown", nil),
		page("vaccination", "/vaccination", "/vaccination", "Vaccination", "PHC vaccination operations: status matrix, cohort detail, shed execution, proof, and verification.", "module-surface",
			[]domain.TableContract{
				table("status-matrix", "Vaccination status matrix", "/vaccination/operations", []string{"cohort", "protocol_cells"}, "matrix_cell"),
				table("cohort-detail", "Per-cohort vaccination detail", "/vaccination/operations", []string{"cohort", "animals", "age_band", "last_dose", "next_due", "status"}, "cohort_row"),
				table("shed-events", "Drive — shed events", "/vaccination/execution", []string{"shed", "owner", "proof", "status", "next_step"}, "shed_event"),
				table("supplier-warmup", "Supplier warmup — Holding Farm", "/procurement/source-entry/loads", []string{"load", "supplier", "holding_farm", "purpose", "evidence", "status"}, "warmup_load"),
			}),
		page("shed-execution", "/vaccination/execution/sheds/{shed_id}", "/vaccination/execution/sheds/{shed_id}", "Vaccination execution", "Full shed execution context for vaccination work.", "record-drilldown",
			[]domain.TableContract{table("shed-drive-rows", "Drive rows", "/vaccination/execution/sheds/{shed_id}", []string{"drive", "cohort", "owner", "proof", "verification", "next_action"}, "shed_event")}),
		page("source-entry", "/procurement/source-entry", "/procurement/source-entry", "Source Entry Board", "Supplier warmup and accepted-intake bridge into PHC vaccination.", "module-surface",
			[]domain.TableContract{table("source-loads", "Supplier warmup — Holding Farm", "/procurement/source-entry/loads", []string{"load", "supplier", "holding_farm", "purpose", "health", "evidence", "status"}, "source_load")}),
		page("source-load", "/procurement/source-entry/loads/{load_id}", "/procurement/source-entry/loads/{load_id}", "Source load", "Full source-entry journey timeline, goat rows, decisions, and arrival gate.", "record-drilldown", nil),
		page("herd-register", "/counts/herd", "/counts/herd", "Herd Register", "Counts entry point for goat registration/import and vaccination trigger proof.", "module-surface",
			[]domain.TableContract{table("herd-register", "Herd Register", "/goats/search", []string{"goat_id", "park", "shed", "breed", "sex", "weight", "lifecycle", "health", "breeding"}, "goat_id")}),
		page("audit-log", "/operations/audit", "/operations/audit", "Audit Log", "Business audit trail for built admin/operator/system actions.", "authority-screen",
			[]domain.TableContract{table("activity-trail", "Activity trail", "/operations/audit", []string{"when", "operator", "operation", "target", "result", "proof"}, "audit_row")}),
		page("config", "/config", "/config", "Config — Protocol Rules", "Admin/Data Ops authority for source-backed protocol rules.", "authority-screen",
			[]domain.TableContract{table("protocol-rules", "Protocol rules", "/protocols", []string{"rule", "status", "scope", "effective_date", "linked_sop", "rule_count"}, "protocol_id")}),
		page("sops", "/sops", "/sops", "SOP Library", "Vaccination SOP policy and form-builder surface.", "authority-screen",
			[]domain.TableContract{table("sop-library", "SOP Library", "/admin/sops", []string{"sop", "domain", "trigger", "steps", "gates", "status"}, "sop_id")}),
		page("goat-passport", "/goats/{goat_id}", "/goats/{goat_id}", "Goat Passport", "Contextual goat identity, timeline, and vaccination passport detail.", "record-drilldown",
			[]domain.TableContract{table("vaccination-history", "Vaccination", "/goats/{goat_id}/passport", []string{"administered", "doses", "route", "status", "proof", "source_obligation"}, "completion_id")}),
	}
}

func page(id, href, pattern, title, subtitle, kind string, tables []domain.TableContract) domain.PageContract {
	if tables == nil {
		tables = []domain.TableContract{}
	}
	return domain.PageContract{
		RouteID: id, Href: href, PathPattern: pattern, Title: title, Subtitle: subtitle, SurfaceKind: kind,
		SourceScope: []string{"current-admin-web-scope", "mock-goatos-dashboard", "active-vaccination-process-integrity-slice"},
		Sections:    []domain.Section{{ID: "primary", Title: title, Kind: kind, ChipKeys: []string{}}},
		Tables:      tables,
		Drawers: []domain.DrawerContract{
			{ID: "record", TitleSource: "selected_row", TriggerParam: "row_param", DataSource: "selected_object", Anatomy: "mock-record-drawer-metagrid", SummaryFields: []string{"title", "status", "owner", "next_action"}, DetailFields: []string{"all_contract_fields"}, FooterActions: []domain.Control{}},
		},
		Controls:        []domain.Control{},
		MigrationStatus: "contract-published",
		ValidationNotes: []string{
			"Frontend may choose compact row fields versus full drawer fields from the same backend object.",
			"Frontend owns layout density, responsive wrapping, focus/open state, and icon token rendering only.",
		},
	}
}

func table(id, title, source string, cols []string, rowParam string) domain.TableContract {
	columns := make([]domain.Column, 0, len(cols))
	for _, col := range cols {
		columns = append(columns, domain.Column{Key: col, Label: humanLabel(col), Visible: true})
	}
	return domain.TableContract{
		ID: id, Title: title, DataSource: source, Columns: columns,
		Filters: []domain.Filter{
			{Key: "search", Label: "Search", Kind: "search", Enabled: true},
			{Key: "filters", Label: "Filters", Kind: "drawer", Enabled: true},
		},
		SortKeys:        []domain.SortKey{},
		PageSizeOptions: []int{5, 10, 25, 50},
		RowClick: domain.RowClickRule{
			Enabled: true, Param: rowParam, TargetDrawer: "record",
			SummaryFields: []string{"id", "title", "status", "owner", "next_action"},
			DetailFields:  []string{"contract_object"},
		},
		SummaryFields: []string{"id", "title", "status", "owner", "next_action"},
		DetailFields:  []string{"contract_object"},
	}
}

func humanLabel(key string) string {
	switch key {
	case "goat_id":
		return "Goat ID"
	case "next_action":
		return "Next action"
	case "effective_date":
		return "Effective date"
	case "linked_sop":
		return "Linked SOP"
	case "rule_count":
		return "Rule count"
	case "source_obligation":
		return "Source obligation"
	default:
		out := []rune(key)
		for i, r := range out {
			if r == '_' {
				out[i] = ' '
			}
		}
		if len(out) > 0 && out[0] >= 'a' && out[0] <= 'z' {
			out[0] = out[0] - ('a' - 'A')
		}
		return string(out)
	}
}

func displayRules() []domain.DisplayRule {
	return []domain.DisplayRule{
		{
			ID:        "summary-vs-detail",
			AppliesTo: []string{"table rows", "cards", "drawers", "matrix cells"},
			Summary:   "Backend sends the object and presentation contract; frontend may render a compact subset in a row/card and the full object in a drawer.",
			FrontendOwns: []string{
				"how many summary fields fit at the current breakpoint",
				"drawer open/closed state and URL search-param selection",
				"responsive wrapping and icon component mapping from backend icon tokens",
			},
			BackendOwns: []string{
				"field labels, enabled/disabled action availability, route visibility, table/filter/sort semantics",
				"which object fields are summary-safe versus detail-only",
				"disabled reasons and empty/error copy",
			},
		},
		{
			ID:           "no-frontend-business-truth",
			AppliesTo:    []string{"navigation", "page titles", "filters", "chips", "sort keys", "actions"},
			Summary:      "Frontend must not invent product words, module availability, server filter semantics, or write-action availability.",
			FrontendOwns: []string{"CSS classes", "hover/focus behavior", "local menu state", "theme preview"},
			BackendOwns:  []string{"labels", "hrefs", "badges", "role lens text", "route labels", "page contracts"},
		},
	}
}

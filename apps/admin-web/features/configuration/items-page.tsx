// A GET form through next/form: Apply is a soft navigation (the page stays on screen), not a document reload.
import Form from "next/form";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { TAP_MIN } from "@/components/minimal/_shared/tap";
import { SegmentTabs } from "@/components/minimal/list/segment-tabs";
import { BookOpen, FileSpreadsheet, Plus, Search, Settings } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { Tag, type Tone } from "@/components/ui-primitives";
import { ProcurementPager } from "@/features/procurement";
import { controlEnabled, copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type {
  ConfigurationColumn,
  ConfigurationListResponse,
  ConfigurationRefOption,
  ConfigurationRegister,
  ConfigurationRegistersResponse,
  ConfigurationRow,
} from "@/lib/api/configuration-server";
import type { ApiResult, ApiUiError } from "@/lib/api/server";
import { all, boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { CatalogueLists, type CatalogueList } from "./catalogue-lists";
import "./configuration-kit.css";
import { RegisterFilter } from "./register-filter";
import { RowActions } from "./row-actions";
import { RowDrawerForm } from "./row-drawer";
import { SheetDrawer } from "./sheet-drawer";
import { WorkbookDrawer } from "./workbook-drawer";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";

/**
 * /configuration/items (maintainer instruction 2026-09-18, from the Claude prototype merged in
 * #297): the farm's reference registers -- farm places, animal types, catalogues -- on ONE page.
 * The rail lists every register with its active count; the table shows the selected one.
 *
 * Everything on screen is backend-owned: the register DEFINITIONS (columns, types, labels, hints,
 * which columns filter) from /admin/configuration/registers, the rows and their composed display
 * names and ref labels from the list read, the copy from the page contract. This file renders a
 * register from its definition and composes no column of its own, which is what lets a new
 * register ship as one backend definition and no page change.
 *
 * Writes are capability-gated on BOTH halves: the four controls on the contract (rendered only
 * when enabled; the backend's reason otherwise) and the route table behind each Server Action.
 *
 * The drawer is client-local: `?edit=new` / `?edit=<row id>` open it from data already on the
 * page through LocalOverlayLink, so opening and closing never re-run this Server Component. The
 * ref selects' options are read ONCE per ref register the selected register needs.
 */

export const ITEMS_PATH = "/configuration/items";
export const PARAM_REGISTER = "register";
export const PARAM_STATUS = "status";
export const PARAM_Q = "q";
export const PARAM_EDIT = "edit";
export const FILTER_PREFIX = "f.";
const PARAM_CURSOR = "cursor";
const PARAM_PAGE = "page";
const PARAM_STACK = "cursor_stack";
const DEFAULT_REGISTER = "parks";

const STATUS_TONE: Record<ConfigurationRow["status"], Tone> = { active: "ok", archived: "mut" };

/** Rebuilds the page URL from the current params with a patch; paging keys are always dropped. */
function href(params: RouteSearchParams, patch: Record<string, string | undefined>, keepPaging = false): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (!keepPaging && (key === PARAM_CURSOR || key === PARAM_PAGE || key === PARAM_STACK)) continue;
    if (key in patch) continue;
    if (Array.isArray(value)) for (const item of value) next.append(key, item);
    else if (value) next.set(key, value);
  }
  for (const [key, value] of Object.entries(patch)) if (value) next.set(key, value);
  const qs = next.toString();
  return qs ? `${ITEMS_PATH}?${qs}` : ITEMS_PATH;
}

/** Switching register drops the search, the status, every filter and the drawer. */
function registerHref(params: RouteSearchParams, register: string): string {
  const patch: Record<string, string | undefined> = { [PARAM_REGISTER]: register, [PARAM_Q]: undefined, [PARAM_STATUS]: undefined, [PARAM_EDIT]: undefined };
  for (const key of Object.keys(params)) if (key.startsWith(FILTER_PREFIX)) patch[key] = undefined;
  return href(params, patch);
}

function nextHref(params: RouteSearchParams, cursor: string | null | undefined): string | null {
  if (!cursor) return null;
  const stack = all(params, PARAM_STACK);
  const current = one(params, PARAM_CURSOR);
  if (current) stack.push(current);
  const page = boundedInt(one(params, PARAM_PAGE), 1, 1, 1000000);
  const base = new URL(href(params, {}), "http://x");
  for (const item of stack.slice(-50)) base.searchParams.append(PARAM_STACK, item);
  base.searchParams.set(PARAM_CURSOR, cursor);
  base.searchParams.set(PARAM_PAGE, String(page + 1));
  return `${base.pathname}${base.search}`;
}

function previousHref(params: RouteSearchParams): string | null {
  const page = boundedInt(one(params, PARAM_PAGE), 1, 1, 1000000);
  if (page <= 1) return null;
  const stack = all(params, PARAM_STACK);
  const previous = stack.pop();
  const base = new URL(href(params, {}), "http://x");
  for (const item of stack) base.searchParams.append(PARAM_STACK, item);
  if (previous) {
    base.searchParams.set(PARAM_CURSOR, previous);
    if (page > 2) base.searchParams.set(PARAM_PAGE, String(page - 1));
  }
  return `${base.pathname}${base.search}`;
}

export type ItemsPageData = {
  registers: ApiResult<ConfigurationRegistersResponse>;
  /** The selected register's page; null when the register is unknown. */
  rows: ApiResult<ConfigurationListResponse> | null;
  /** Ref options per register the selected register's columns point at. */
  options: Record<string, ConfigurationRefOption[]>;
  /** The catalogue layout's lists (every category, active and archived, with counts); null otherwise. */
  lists: ConfigurationRow[] | null;
  /** The reference_lists row of the open dynamic register (its name, description); null otherwise. */
  openList: ConfigurationRow | null;
  /** Non-primary reads needed to render filters/drawers; any failure must stay visible. */
  loadErrors: ApiUiError[];
};

/** The catalogue layout's drawer ids for a list: `cat:new` / `cat:<id>`, beside the items' own ids. */
const LIST_EDIT_PREFIX = "cat:";
/** The sheet drawer id (download / upload of the open register). */
const SHEET_EDIT_ID = "sheet";
/** The onboarding workbook drawer id (one Excel, one tab per list). */
const WORKBOOK_EDIT_ID = "workbook";
/** The reference-lists drawer ids: `reflist:new` (Add list) / `reflist:<key>` (the open list itself). */
const REFLIST_EDIT_PREFIX = "reflist:";
const REFERENCE_GROUP = "reference_lists";

/** The parameters the page reads for its data, resolved once so page.tsx and the feature agree. */
export function itemsPageParams(sp: RouteSearchParams, pageContract: AdminUiPageContract) {
  const filters: Record<string, string> = {};
  for (const [key, value] of Object.entries(sp)) {
    if (!key.startsWith(FILTER_PREFIX)) continue;
    const v = Array.isArray(value) ? value[0] : value;
    if (v) filters[key.slice(FILTER_PREFIX.length)] = v;
  }
  const status = one(sp, PARAM_STATUS);
  return {
    register: one(sp, PARAM_REGISTER) || DEFAULT_REGISTER,
    status: status === "archived" || status === "all" ? status : "active",
    q: one(sp, PARAM_Q) || undefined,
    cursor: one(sp, PARAM_CURSOR) || undefined,
    limit: tablePageSizes(pageContract, "configuration-rows")[0] ?? 25,
    filters,
  };
}

/** Which ref registers the selected register's columns point at (the options the drawer and filters need). */
export function refRegistersOf(register: ConfigurationRegister | undefined): string[] {
  if (!register) return [];
  const out: string[] = [];
  for (const column of register.columns) if (column.type === "ref" && column.ref && !out.includes(column.ref)) out.push(column.ref);
  return out;
}

function fieldText(row: ConfigurationRow, column: ConfigurationColumn, placeholder: string, yes: string, no: string): string {
  const value = row.fields[column.key];
  if (column.type === "ref") return row.labels[column.key] ?? (value ? String(value) : placeholder);
  if (value === null || value === undefined || value === "") return placeholder;
  if (column.type === "bool") return value ? yes : no;
  if (column.type === "enum") return column.options?.find((option) => option.value === String(value))?.label ?? String(value);
  return String(value);
}

export function ItemsPage({ searchParams, pageContract, data }: { searchParams?: RouteSearchParams; pageContract: AdminUiPageContract; data: ItemsPageData }) {
  const sp = searchParams ?? {};
  const c = (key: string) => copy(pageContract, key);
  const canCreate = controlEnabled(pageContract, "create_row", false);
  const canEdit = controlEnabled(pageContract, "edit_row", false);
  const canSetStatus = controlEnabled(pageContract, "set_row_status", false);
  const canDelete = controlEnabled(pageContract, "delete_row", false);
  const canWrite = canCreate || canEdit || canSetStatus || canDelete;
  const canExport = controlEnabled(pageContract, "export_sheet", false);
  const canImport = controlEnabled(pageContract, "import_sheet", false);
  const canExportWorkbook = controlEnabled(pageContract, "export_workbook", false);
  const canImportWorkbook = controlEnabled(pageContract, "import_workbook", false);

  const params = itemsPageParams(sp, pageContract);
  const catalog = data.registers.ok ? data.registers.data : null;
  const registers = catalog?.registers ?? [];
  const register = registers.find((item) => item.key === params.register);
  const listHref = href(sp, { [PARAM_EDIT]: undefined }, true);
  const editHref = (id: string) => href(sp, { [PARAM_EDIT]: id }, true);
  const page = data.rows && data.rows.ok ? data.rows.data : null;
  const rows = page?.rows ?? [];
  const pageNo = boundedInt(one(sp, PARAM_PAGE), 1, 1, 1000000);
  const placeholder = "—";
  const writable = !!register && !register.read_only;
  // THE ROW'S OWN ACTIONS (maintainer instruction 2026-09-22). The column appears only when this
  // person can actually do something to a row of this register, so a reader is never shown a menu
  // whose every item would be refused.
  const rowsWritable = writable;
  const rowActionsOffered = rowsWritable && (canEdit || canSetStatus || canDelete);
  const rowActionLabels = {
    edit: c("action.edit_row.label"),
    deactivate: c("action.archive"),
    activate: c("action.restore"),
    remove: c("action.delete"),
    cancel: c("action.cancel"),
    confirmRemove: c("drawer.delete_confirm"),
    more: c("action.row_menu"),
    failed: c("action.failed_message"),
  };

  // Table columns: the display name first, then every column not hidden from the list and not the
  // one the display already shows, then what the row holds, then status.
  const nameKey = register?.display_column ?? register?.columns.find((column) => column.key === "name" || column.key === "label")?.key;
  // A kind-scoped item column (route, disease, ...) earns its place in the table only when a row
  // on this page carries a value: seven vaccines beside four empty medicine columns say nothing.
  const listColumns = (register?.columns ?? []).filter((column) => {
    if (column.list_hidden || column.key === nameKey) return false;
    if (!column.kinds || !column.kinds.length) return true;
    return rows.some((row) => {
      const value = row.fields[column.key];
      return value !== null && value !== undefined && value !== "";
    });
  });
  const hasCounts = rows.some((row) => row.counts && Object.keys(row.counts).length > 0);
  const displayColumnLabel = (register?.display_column ? register.columns.find((column) => column.key === register.display_column)?.label : "") || c("column.display");

  const filterColumns = (register?.filters ?? []).map((key) => register?.columns.find((column) => column.key === key)).filter((column): column is ConfigurationColumn => !!column);

  const drawerItems: LocalOverlayDrawerItem[] = [];
  if (register && writable && canCreate) {
    drawerItems.push({
      id: "new",
      eyebrow: register.label,
      title: `${c("drawer.create_title")} ${register.one.toLowerCase()}`,
      icon: <Settings className="ic" aria-hidden="true" />,
      body: <RowDrawerForm pageContract={pageContract} register={register} options={data.options} canEdit={canCreate} canSetStatus={false} canDelete={false} listHref={listHref} />,
    });
  }
  if (register) {
    for (const row of rows) {
      // A feed row is folded in from Feed Config and edited there.
      const rowReadOnly = row.fields.read_only === true;
      drawerItems.push({
        id: row.id,
        eyebrow: register.label,
        title: writable && canEdit && !rowReadOnly ? `${c("drawer.edit_title")} ${register.one.toLowerCase()}` : row.display,
        icon: <Settings className="ic" aria-hidden="true" />,
        body: (
          <RowDrawerForm
            pageContract={pageContract}
            register={register}
            row={row}
            options={data.options}
            canEdit={writable && canEdit && !rowReadOnly}
            canSetStatus={writable && canSetStatus && !rowReadOnly}
            canDelete={writable && canDelete && !rowReadOnly}
            listHref={listHref}
            editElsewhere={rowReadOnly ? { href: "/feed/config", label: c("action.edit_elsewhere") + " Feed Config" } : undefined}
          />
        ),
      });
    }
  }
  const listsRegister = registers.find((item) => item.key === "categories");
  const isCatalogue = register?.layout === "catalogue" && !!listsRegister;
  // The lists, in tree order, each counting its whole subtree (the prototype's "Vaccines 7").
  const catalogueLists: CatalogueList[] = [];
  // The chosen list's root and its sub-lists, rendered on the right above the items.
  let selectedRoot: ConfigurationRow | null = null;
  const subLists: { id: string; name: string; depth: number; count: number; href: string; editHref: string }[] = [];
  if (isCatalogue) {
    const all = data.lists ?? [];
    const childrenOf = new Map<string, ConfigurationRow[]>();
    for (const list of all) {
      const parent = String(list.fields.parent_id ?? "");
      childrenOf.set(parent, [...(childrenOf.get(parent) ?? []), list]);
    }
    const subtreeCount = (id: string): number => (all.find((l) => l.id === id)?.counts?.items ?? 0) + (childrenOf.get(id) ?? []).reduce((sum, child) => sum + subtreeCount(child.id), 0);
    // A list sits under CATALOGUES when its ROOT is built in (Medicines > Antibiotics stays with
    // Medicines), under YOUR LISTS when the farm made the root.
    // The panel carries only the top-level lists (maintainer instruction 2026-09-18); a list's
    // sub-lists show on the right, above its items, once it is chosen.
    for (const list of childrenOf.get("") ?? []) {
      catalogueLists.push({
        id: list.id,
        name: String(list.fields.name ?? list.display),
        depth: 0,
        builtin: list.is_builtin,
        locked: list.is_builtin,
        archived: list.status === "archived",
        count: subtreeCount(list.id),
        href: href(sp, { [FILTER_PREFIX + "category_id"]: list.id, [PARAM_EDIT]: undefined }),
        editHref: href(sp, { [PARAM_EDIT]: LIST_EDIT_PREFIX + list.id }, true),
      });
    }
    const selectedId = params.filters.category_id ?? "";
    let rootId = selectedId;
    for (let guard = 0; guard < 8 && rootId; guard += 1) {
      const parent = String(all.find((l) => l.id === rootId)?.fields.parent_id ?? "");
      if (!parent) break;
      rootId = parent;
    }
    selectedRoot = all.find((l) => l.id === rootId) ?? null;
    if (selectedRoot) {
      const root = selectedRoot;
      const collect = (parent: string, depth: number) => {
        for (const list of childrenOf.get(parent) ?? []) {
          if (list.status === "archived") continue;
          subLists.push({ id: list.id, name: String(list.fields.name ?? list.display), depth, count: subtreeCount(list.id), href: href(sp, { [FILTER_PREFIX + "category_id"]: list.id, [PARAM_EDIT]: undefined }), editHref: href(sp, { [PARAM_EDIT]: LIST_EDIT_PREFIX + list.id }, true) });
          collect(list.id, depth + 1);
        }
      };
      collect(root.id, 0);
    }
  }
  // The catalogue layout also edits its LISTS (the categories register) from the same page.
  if (isCatalogue && listsRegister) {
    if (canCreate && selectedRoot) {
      drawerItems.push({
        id: LIST_EDIT_PREFIX + "new:" + selectedRoot.id,
        eyebrow: listsRegister.label,
        title: `${c("drawer.create_title")} ${listsRegister.one.toLowerCase()}`,
        icon: <Settings className="ic" aria-hidden="true" />,
        body: <RowDrawerForm pageContract={pageContract} register={listsRegister} options={data.options} canEdit={canCreate} canSetStatus={false} canDelete={false} listHref={listHref} defaults={{ parent_id: selectedRoot.id }} />,
      });
    }
    if (canCreate) {
      drawerItems.push({
        id: LIST_EDIT_PREFIX + "new",
        eyebrow: listsRegister.label,
        title: `${c("drawer.create_title")} ${listsRegister.one.toLowerCase()}`,
        icon: <Settings className="ic" aria-hidden="true" />,
        body: <RowDrawerForm pageContract={pageContract} register={listsRegister} options={data.options} canEdit={canCreate} canSetStatus={false} canDelete={false} listHref={listHref} />,
      });
    }
    for (const list of data.lists ?? []) {
      drawerItems.push({
        id: LIST_EDIT_PREFIX + list.id,
        eyebrow: listsRegister.label,
        title: canEdit ? `${c("drawer.edit_title")} ${listsRegister.one.toLowerCase()}` : list.display,
        icon: <Settings className="ic" aria-hidden="true" />,
        body: <RowDrawerForm pageContract={pageContract} register={listsRegister} row={list} options={data.options} canEdit={canEdit} canSetStatus={canSetStatus} canDelete={canDelete} listHref={listHref} />,
      });
    }
  }
  // The bulk sheet drawer: download (on read) and upload (on write) of the open register.
  if (register && canExport && !register.hidden) {
    drawerItems.push({
      id: SHEET_EDIT_ID,
      eyebrow: register.label,
      title: c("sheet.title"),
      icon: <FileSpreadsheet className="ic" aria-hidden="true" />,
      body: <SheetDrawer pageContract={pageContract} register={register} canWrite={canImport} />,
    });
  }
  // The onboarding workbook drawer: the whole setup as one Excel file, a tab per list.
  if (canExportWorkbook) {
    const registerLabels: Record<string, string> = {};
    for (const item of registers) registerLabels[item.key] = item.label;
    drawerItems.push({
      id: WORKBOOK_EDIT_ID,
      eyebrow: c("crumb"),
      title: c("workbook.title"),
      icon: <BookOpen className="ic" aria-hidden="true" />,
      body: <WorkbookDrawer pageContract={pageContract} canWrite={canImportWorkbook} registerLabels={registerLabels} />,
    });
  }
  // Reference lists: the "Add list" drawer, and the open list's own drawer (rename / archive /
  // delete the list, not its entries).
  const referenceListsRegister = registers.find((item) => item.key === "reference_lists");
  if (referenceListsRegister && canCreate) {
    drawerItems.push({
      id: REFLIST_EDIT_PREFIX + "new",
      eyebrow: referenceListsRegister.label,
      title: `${c("drawer.create_title")} ${referenceListsRegister.one.toLowerCase()}`,
      icon: <Settings className="ic" aria-hidden="true" />,
      body: <RowDrawerForm pageContract={pageContract} register={referenceListsRegister} options={data.options} canEdit={canCreate} canSetStatus={false} canDelete={false} listHref={listHref} />,
    });
  }
  if (referenceListsRegister && register?.list_key && data.openList) {
    drawerItems.push({
      id: REFLIST_EDIT_PREFIX + register.list_key,
      eyebrow: referenceListsRegister.label,
      title: canEdit ? `${c("drawer.edit_title")} ${referenceListsRegister.one.toLowerCase()}` : data.openList.display,
      icon: <Settings className="ic" aria-hidden="true" />,
      body: <RowDrawerForm pageContract={pageContract} register={referenceListsRegister} row={data.openList} options={data.options} canEdit={canEdit} canSetStatus={canSetStatus} canDelete={canDelete} listHref={listHref} />,
    });
  }
  const departmentColumn = register?.columns.find((column) => column.key === "department");
  const departmentLabel = (value: unknown) => departmentColumn?.options?.find((option) => option.value === String(value ?? ""))?.label ?? "";

  const groups = catalog?.groups ?? [];

  return (
    <div className="screen on cfg-items">
      <PageHeader title={pageContract.title} crumbs={[{ label: c("crumb") }, { label: pageContract.title }]} />

      {!data.registers.ok ? (
        <Alert severity="error">
          <b>{data.registers.error.code ?? data.registers.error.kind}</b>&nbsp;{data.registers.error.message}
        </Alert>
      ) : null}
      {data.loadErrors.map((error, index) => (
        <Alert severity="error" key={`${error.code ?? error.kind}-${index}`}>
          <b>{error.code ?? error.kind}</b>&nbsp;{error.message}
        </Alert>
      ))}

      <div className={isCatalogue ? "cfg-layout cfg-layout-3" : "cfg-layout"}>
        <aside className="card cfg-rail" aria-label={c("rail.title")}>
          {groups.map((group) => {
            const members = registers.filter((item) => item.group === group.key && !item.hidden);
            if (!members.length && group.key !== REFERENCE_GROUP) return null;
            return (
              <div key={group.key} className="cfg-rail-group">
                <div className="cfg-rail-title">{group.label}</div>
                {members.map((item) => {
                  const active = item.key === params.register;
                  return (
                    <Link key={item.key} href={registerHref(sp, item.key)} scroll={false} className={active ? "cfg-rail-item on" : "cfg-rail-item"} aria-current={active ? "page" : undefined}>
                      <span>{item.label}</span>
                      <span className="cfg-rail-count">{catalog?.counts[item.key] ?? 0}</span>
                    </Link>
                  );
                })}
                {group.key === REFERENCE_GROUP && referenceListsRegister && canCreate ? (
                  <LocalOverlayLink href={href(sp, { [PARAM_EDIT]: REFLIST_EDIT_PREFIX + "new" }, true)} scroll={false} className="cfg-rail-add">
                    <Plus className="ic" aria-hidden="true" /> {c("reference.add_list")}
                  </LocalOverlayLink>
                ) : null}
              </div>
            );
          })}
        </aside>

        {isCatalogue ? (
          <CatalogueLists
            copy={{
              "lists.title": c("lists.title"),
              "lists.all": c("lists.all"),
              "lists.catalogues": c("lists.catalogues"),
              "lists.yours": c("lists.yours"),
              "lists.new": c("lists.new"),
              "lists.search": c("lists.search"),
              "column.status": c("column.status"),
              "status.active": c("status.active"),
              "status.archived": c("status.archived"),
              "action.edit_row.label": c("action.edit_row.label"),
            }}
            lists={catalogueLists}
            allHref={href(sp, { [FILTER_PREFIX + "category_id"]: undefined, [PARAM_EDIT]: undefined })}
            allCount={catalog?.counts[register?.key ?? ""] ?? 0}
            current={selectedRoot?.id ?? ""}
            canEdit={canEdit}
            newHref={href(sp, { [PARAM_EDIT]: LIST_EDIT_PREFIX + "new" }, true)}
          />
        ) : null}

        <section className="card cfg-main kit-tablecard" aria-label={register?.label ?? c("crumb")}>
          <div className="hd cfg-main-hd" style={{ flexWrap: "wrap" }}>
            <div>
              <h3>
                {register?.label ?? params.register}{" "}
                {/* Same number as the rail: the rail counts ACTIVE rows, so the header shows the
                    active count on the default view and the page's own total under a status filter. */}
                {page ? <Tag tone="mut">{params.status === "active" && register ? (catalog?.counts[register.key] ?? page.total) : page.total}</Tag> : null}
              </h3>
            </div>
            <div className="sp" style={{ flex: 1 }} />
            {register?.list_key && canEdit && data.openList ? (
              <LocalOverlayLink href={href(sp, { [PARAM_EDIT]: REFLIST_EDIT_PREFIX + register.list_key }, true)} scroll={false} className="btn sm ghost">
                {c("reference.edit_list")}
              </LocalOverlayLink>
            ) : null}
            {register && canExport && !register.hidden ? (
              <LocalOverlayLink href={editHref(SHEET_EDIT_ID)} scroll={false} className="btn sm ghost" data-testid="sheet-open">
                <FileSpreadsheet className="ic" style={{ width: 14 }} aria-hidden="true" />
                {c("sheet.title")}
              </LocalOverlayLink>
            ) : null}
            {canExportWorkbook ? (
              <LocalOverlayLink href={editHref(WORKBOOK_EDIT_ID)} scroll={false} className="btn sm ghost" data-testid="workbook-open">
                <BookOpen className="ic" style={{ width: 14 }} aria-hidden="true" />
                {c("workbook.open")}
              </LocalOverlayLink>
            ) : null}
            {register?.read_only && register.edit_href ? (
              <Link href={register.edit_href} className="btn sm">
                {c("action.edit_elsewhere")} {register.edit_label ?? register.edit_href}
              </Link>
            ) : null}
            {register && writable && canCreate ? (
              <Button component={LocalOverlayLink} href={editHref("new")} scroll={false} variant="contained" color="primary" size="small" startIcon={<Plus size={14} aria-hidden="true" />}>
                {c("action.create_row.label")} {register.one.toLowerCase()}
              </Button>
            ) : null}
          </div>
          {!canWrite ? <div className="note" style={{ margin: "10px 16px 0" }}>{c("configure.disabled_no_access")}</div> : null}

          {isCatalogue && selectedRoot ? (
            <div className="cfg-sublists" aria-label={c("lists.sublists")}>
              <div className="subtabs">
                <Link href={href(sp, { [FILTER_PREFIX + "category_id"]: selectedRoot.id, [PARAM_EDIT]: undefined })} scroll={false} className={params.filters.category_id === selectedRoot.id ? "on" : ""}>
                  {c("lists.all_in")} {String(selectedRoot.fields.name ?? selectedRoot.display)}
                </Link>
                {subLists.map((list) => (
                  <Link key={list.id} href={list.href} scroll={false} className={params.filters.category_id === list.id ? "on" : ""} style={{ marginLeft: list.depth * 10 }}>
                    {list.name} <span className="cbq">{list.count}</span>
                  </Link>
                ))}
              </div>
              {canEdit && params.filters.category_id && params.filters.category_id !== selectedRoot.id ? (
                <LocalOverlayLink href={href(sp, { [PARAM_EDIT]: LIST_EDIT_PREFIX + params.filters.category_id }, true)} scroll={false} className="btn sm ghost">
                  {c("action.edit_row.label")}
                </LocalOverlayLink>
              ) : null}
              {canCreate ? (
                <LocalOverlayLink href={href(sp, { [PARAM_EDIT]: LIST_EDIT_PREFIX + "new:" + selectedRoot.id }, true)} scroll={false} className="btn sm ghost">
                  <Plus className="ic" style={{ width: 13 }} aria-hidden="true" /> {c("lists.new_under")} {String(selectedRoot.fields.name ?? selectedRoot.display)}
                </LocalOverlayLink>
              ) : null}
            </div>
          ) : null}

          <div className="tbar">
            <Form action={ITEMS_PATH} prefetch={false} className="tsearch" role="search">
              <input type="hidden" name={PARAM_REGISTER} value={params.register} />
              {params.status !== "active" ? <input type="hidden" name={PARAM_STATUS} value={params.status} /> : null}
              {Object.entries(params.filters).map(([key, value]) => (
                <input key={key} type="hidden" name={FILTER_PREFIX + key} value={value} />
              ))}
              <Search className="ic" aria-hidden="true" />
              <input name={PARAM_Q} defaultValue={params.q ?? ""} placeholder={`${c("search.placeholder")} ${(register?.label ?? "").toLowerCase()}`} aria-label={c("search.placeholder")} />
            </Form>
            {filterColumns.filter((column) => !(isCatalogue && column.key === "category_id")).map((column) => {
              const enumOptions: ConfigurationRefOption[] | null = column.type === "enum" ? (column.options ?? []).map((option) => ({ id: option.value, label: option.label })) : null;
              const opts = (enumOptions ?? data.options[column.ref ?? ""] ?? []).filter((option) => {
                // A pen filter narrows to the chosen park when both filters are offered.
                if (column.key === "pen_id" && params.filters.park_id) return option.parent_id === params.filters.park_id;
                return true;
              });
              const hrefFor: Record<string, string> = { "": href(sp, { [FILTER_PREFIX + column.key]: undefined, ...(column.key === "park_id" ? { [FILTER_PREFIX + "pen_id"]: undefined } : {}) }) };
              for (const option of opts) hrefFor[option.id] = href(sp, { [FILTER_PREFIX + column.key]: option.id, ...(column.key === "park_id" ? { [FILTER_PREFIX + "pen_id"]: undefined } : {}) });
              const label = column.key === "department" ? c("filter.department.all") : column.label;
              return <RegisterFilter key={column.key} label={label} allLabel={copy(pageContract, "filter.all", "All")} current={params.filters[column.key] ?? ""} options={opts.map((option) => ({ value: option.id, label: option.label }))} hrefFor={hrefFor} />;
            })}
            <div className="sp" style={{ flex: 1 }} />
            <SegmentTabs
              ariaLabel={c("column.status")}
              value={params.status}
              keepScroll
              tabs={(["active", "archived", "all"] as const).map((status) => ({ value: status, label: c(`status.${status}`), href: href(sp, { [PARAM_STATUS]: status === "active" ? undefined : status }) }))}
            />
          </div>

          <div className="bd">
            {data.rows && !data.rows.ok ? (
              <Alert severity="error">
                <b>{data.rows.error.code ?? data.rows.error.kind}</b>&nbsp;{data.rows.error.message}
              </Alert>
            ) : null}
            {!register ? (
              <EmptyState title={c("empty.rows")} />
            ) : rows.length === 0 ? (
              <EmptyState title={params.q || Object.keys(params.filters).length ? c("empty.search") : c("empty.rows")} />
            ) : (
              <div className="tablewrap" tabIndex={0} role="group" aria-label={register.label}>
                <Table className="tbl">
                  <TableHead>
                    <TableRow>
                      {/* A register that names its own display column is headed by THAT column's
                          own label, which the backend contract carries: the Animals table used to
                          file an animal tag under the generic header, and an animal has no name. */}
                      <TableCell component="th">{isCatalogue ? c("column.item") : displayColumnLabel}</TableCell>
                      {isCatalogue ? <TableCell component="th">{c("column.tracking")}</TableCell> : null}
                      {(isCatalogue ? [] : listColumns).map((column) => (
                        <TableCell component="th" key={column.key}>{column.label}</TableCell>
                      ))}
                      {hasCounts && !isCatalogue ? <TableCell component="th">{c("column.counts")}</TableCell> : null}
                      <TableCell component="th">{c("column.status")}</TableCell>
                      {/* The actions column carries no heading: its buttons name themselves, and a
                          heading over a 2-button cell reads as a data column that is always blank. */}
                      {rowActionsOffered ? <TableCell component="th" aria-label={c("action.row_actions")} /> : null}
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {rows.map((row) => (
                      <TableRow key={row.id} className={row.status === "archived" ? "cfg-archived" : undefined}>
                        {/* Phone: the row link is a 44px tap target, not a 16px inline word. */}
                        <TableCell sx={{ "& .cfg-row-link": { display: { xs: "flex", md: "inline" }, alignItems: "center", minHeight: { xs: TAP_MIN, md: 0 } } }}>
                          <LocalOverlayLink href={editHref(row.id)} scroll={false} className="cfg-row-link">
                            <b>{row.display}</b>
                          </LocalOverlayLink>
                          {row.is_builtin ? (
                            <span className="muted small" style={{ marginLeft: 8 }}>
                              {c("tag.builtin")}
                            </span>
                          ) : null}
                          {isCatalogue ? (
                            <div className="muted small">
                              {[String(row.fields.unit ?? ""), row.labels.category_id ?? "", departmentLabel(row.fields.department)].filter(Boolean).join(" · ")}
                            </div>
                          ) : null}
                        </TableCell>
                        {isCatalogue ? <TableCell>{row.fields.tracking ? <Tag tone="mut">{String(row.fields.tracking)}</Tag> : null}</TableCell> : null}
                        {(isCatalogue ? [] : listColumns).map((column) => (
                          <TableCell key={column.key} className={column.type === "number" ? "num" : undefined}>
                            {column.type === "code" || column.key === "code" ? <span className="mono muted">{fieldText(row, column, placeholder, c("value.yes"), c("value.no"))}</span> : fieldText(row, column, placeholder, c("value.yes"), c("value.no"))}
                          </TableCell>
                        ))}
                        {hasCounts && !isCatalogue ? (
                          <TableCell className="muted small">
                            {row.counts
                              ? Object.entries(row.counts)
                                  .filter(([, n]) => n > 0)
                                  .map(([noun, n]) => `${n} ${noun.replace(/_/g, " ")}`)
                                  .join(" · ") || placeholder
                              : placeholder}
                          </TableCell>
                        ) : null}
                        <TableCell>
                          <Tag tone={STATUS_TONE[row.status]}>{c(`status.${row.status}`)}</Tag>
                        </TableCell>
                        {rowActionsOffered ? (
                          <TableCell className="cfg-rowacts-cell">
                            <RowActions
                              register={params.register}
                              rowId={row.id}
                              rowVersion={row.row_version}
                              status={row.status}
                              isBuiltin={row.is_builtin}
                              editHref={editHref(row.id)}
                              canEdit={rowsWritable && canEdit && row.fields.read_only !== true}
                              canSetStatus={rowsWritable && canSetStatus && row.fields.read_only !== true}
                              canDelete={rowsWritable && canDelete && row.fields.read_only !== true}
                              labels={rowActionLabels}
                            />
                          </TableCell>
                        ) : null}
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
            {/* Kit footer wording: the slice this page shows against the whole-filter total the
                backend reports; cursor paging, so the arrows are real links. */}
            <ProcurementPager
              prevHref={previousHref(sp)}
              nextHref={nextHref(sp, page?.next_cursor)}
              page={pageNo}
              count={rows.length}
              noun={c("pager.noun").replace(/s$/, "")}
              forceVisible={rows.length > 0}
              rangeLabel={rows.length === 0 ? "0" : `${(pageNo - 1) * params.limit + 1}–${(pageNo - 1) * params.limit + rows.length} ${copy(pageContract, "pager.of", "of")} ${page?.total ?? rows.length}`}
            />
          </div>
        </section>
      </div>

      <LocalOverlayDrawer items={drawerItems} selectionKey={PARAM_EDIT} initialSelectedId={one(sp, PARAM_EDIT)} closeHref={listHref} ariaLabel={register?.label ?? c("crumb")} closeLabel={c("action.close")} />
    </div>
  );
}

"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import { useCallback, useMemo, useState, type ReactNode } from "react";
import Link from "@/components/no-prefetch-link";
import {
  ArrowUpDown,
  Columns3,
  Copy,
  Download,
  ExternalLink,
  Paperclip,
  X,
} from "lucide-react";
import Button from "@mui/material/Button";
import { RowMenu } from "@/components/app/row-menu";
import { FilterBar } from "@/components/app/filter-bar";
import { TableFooter } from "@/components/app/table-footer";
import { DenseToggle } from "@/components/app/dense-toggle";
import { DateRangeField } from "@/components/app/date-range-field";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import type { TaskRow } from "./types";
import Checkbox from "@mui/material/Checkbox";
import TableSortLabel from "@mui/material/TableSortLabel";

/** The real table's columns, in render order. The route skeleton is built from this same list. */
export const TASK_COLUMNS = [
  { id: "task", label: "Task", width: "34%", sortable: true, className: "lt-task-col" },
  { id: "days", label: "Days", width: "14%", sortable: true, className: "lt-days-col" },
  { id: "assignee", label: "Assignee", width: "16%", sortable: true, className: "lt-people-col" },
  { id: "raised", label: "Raised by", width: "14%", sortable: true, className: "lt-raised-col lt-people-col" },
  { id: "status", label: "Status", width: "10%", sortable: true, className: "" },
  { id: "evidence", label: "Evidence", width: "12%", sortable: false, className: "lt-evidence-col" },
] as const;

type SortId = (typeof TASK_COLUMNS)[number]["id"];

export type TaskTableProps = {
  tasks: TaskRow[];
  selectedId?: string;
  /** Href for a row, e.g. `/tasks?scope=team_progress&task=<id>`. */
  rowHref: (task: TaskRow) => string;
  pageSizeOptions: number[];
  /** Per-task deadline clock and status cells, supplied by the server page. */
  renderClock: (task: TaskRow, compact: boolean) => ReactNode;
  renderStatus: (task: TaskRow) => ReactNode;
  copyFor: (key: string, fallback: string) => string;
};

/**
 * The leadership task table with the spec §2 toolbar and spec §3/§4 table anatomy.
 *
 * Filtering, sorting and paging are CLIENT-SIDE over the 50 rows the route already fetched — this
 * component never issues a request and never mutates anything. Status changes stay where they were,
 * in the detail panel's server actions.
 */
export function TaskTable({ tasks, selectedId, rowHref, pageSizeOptions, renderClock, renderStatus, copyFor }: TaskTableProps) {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [assignee, setAssignee] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [sort, setSort] = useState<{ id: SortId; dir: "asc" | "desc" }>({ id: "days", dir: "asc" });
  const [dense, setDense] = useState(false);
  const [selection, setSelection] = useState<string[]>([]);
  const [page, setPage] = useState(1);
  const [rows, setRows] = useState(pageSizeOptions[0] ?? 10);

  const assigneeOptions = useMemo(() => {
    const names = Array.from(new Set(tasks.map((t) => t.assignee).filter(Boolean))).sort();
    return [{ value: "", label: copyFor("filter.assignee.all", "All assignees") }, ...names.map((n) => ({ value: n, label: n }))];
  }, [tasks, copyFor]);

  const statusOptions = useMemo(
    () => [
      { value: "", label: copyFor("filter.status.all", "All statuses") },
      { value: "open", label: copyFor("status.open", "Open") },
      { value: "doing", label: copyFor("status.doing", "Doing") },
      { value: "done", label: copyFor("status.done", "Done") },
    ],
    [copyFor],
  );

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    // The deadline is rendered pre-formatted by the backend (dd/mm/yyyy hh:mm); parse it back for
    // the range filter rather than inventing a second date field.
    const stamp = (label: string): number | null => {
      const m = /^(\d{2})\/(\d{2})\/(\d{4})/.exec(label);
      return m ? Date.parse(`${m[3]}-${m[2]}-${m[1]}`) : null;
    };
    const fromMs = from ? Date.parse(from) : null;
    const toMs = to ? Date.parse(to) : null;
    return tasks.filter((t) => {
      if (q && !`${t.number} ${t.title} ${t.assignee} ${t.raisedBy}`.toLowerCase().includes(q)) return false;
      if (status && t.status !== status) return false;
      if (assignee && t.assignee !== assignee) return false;
      if (fromMs !== null || toMs !== null) {
        const d = stamp(t.deadlineLabel);
        if (d === null) return false;
        if (fromMs !== null && d < fromMs) return false;
        if (toMs !== null && d > toMs) return false;
      }
      return true;
    });
  }, [tasks, query, status, assignee, from, to]);

  const sorted = useMemo(() => {
    const key = (t: TaskRow): string | number => {
      switch (sort.id) {
        case "days":
          return t.daysLeft ?? Number.POSITIVE_INFINITY;
        case "assignee":
          return t.assignee.toLowerCase();
        case "raised":
          return t.raisedBy.toLowerCase();
        case "status":
          return t.status;
        default:
          return t.title.toLowerCase();
      }
    };
    const dir = sort.dir === "asc" ? 1 : -1;
    return [...filtered].sort((a, b) => {
      const ka = key(a);
      const kb = key(b);
      if (ka === kb) return 0;
      return ka > kb ? dir : -dir;
    });
  }, [filtered, sort]);

  const total = sorted.length;
  const pages = Math.max(1, Math.ceil(total / rows));
  const current = Math.min(page, pages);
  const visible = sorted.slice((current - 1) * rows, current * rows);

  const chips: Array<{ id: string; label: string; clear: () => void }> = [];
  if (query.trim()) chips.push({ id: "q", label: `"${query.trim()}"`, clear: () => setQuery("") });
  if (status) chips.push({ id: "s", label: statusOptions.find((o) => o.value === status)?.label ?? status, clear: () => setStatus("") });
  if (assignee) chips.push({ id: "a", label: assignee, clear: () => setAssignee("") });
  if (from || to) chips.push({ id: "d", label: `${from || "…"} → ${to || "…"}`, clear: () => { setFrom(""); setTo(""); } });

  const clearAll = useCallback(() => {
    setQuery("");
    setStatus("");
    setAssignee("");
    setFrom("");
    setTo("");
    setPage(1);
  }, []);

  const toggleSort = (id: SortId) => {
    setSort((s) => (s.id === id ? { id, dir: s.dir === "asc" ? "desc" : "asc" } : { id, dir: "asc" }));
    setPage(1);
  };

  const exportCsv = useCallback(() => {
    const head = ["number", "title", "assignee", "role", "raised_by", "status", "deadline", "attachments"];
    const body = sorted.map((t) => [t.number, t.title, t.assignee, t.assigneeRole, t.raisedBy, t.status, t.deadlineLabel, t.attachments]);
    const csv = [head, ...body].map((r) => r.map((c) => `"${String(c).replace(/"/g, '""')}"`).join(",")).join("\r\n");
    const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "tasks.csv";
    a.click();
    URL.revokeObjectURL(url);
  }, [sorted]);

  const allShownSelected = visible.length > 0 && visible.every((t) => selection.includes(t.id));

  return (
    <>
      <FilterBar
        className="lt-toolbar"
        search={{
          value: query,
          placeholder: copyFor("filter.search_placeholder", "Search tasks"),
          ariaLabel: copyFor("filter.search_label", "Search tasks"),
          onChange: (v) => {
            setQuery(v);
            setPage(1);
          },
        }}
        actions={
          <>
            <Button color="primary" variant="text" size="small" startIcon={<Columns3 size={16} />} onClick={() => setDense((d) => !d)}>
              {copyFor("action.columns", "Columns")}
            </Button>
            <Button color="primary" variant="text" size="small" startIcon={<Download size={16} />} onClick={exportCsv}>
              {copyFor("action.export", "Export")}
            </Button>
            <RowMenu
              ariaLabel={copyFor("action.more", "More actions")}
              actions={[
                { label: copyFor("action.reset_filters", "Reset filters"), icon: <X size={15} />, onSelect: clearAll, disabled: chips.length === 0 },
                { label: dense ? copyFor("action.comfortable", "Comfortable rows") : copyFor("action.dense", "Dense rows"), icon: <ArrowUpDown size={15} />, onSelect: () => setDense((d) => !d) },
              ]}
            />
          </>
        }
        summary={
          <div className="lt-chiprow">
            <span className="muted small">{total} / {tasks.length}</span>
            {chips.map((c) => (
              <span key={c.id} className="lt-fchip">
                {c.label}
                <button type="button" aria-label={`${copyFor("action.remove_filter", "Remove filter")}: ${c.label}`} onClick={() => { c.clear(); setPage(1); }}>
                  <X size={12} aria-hidden="true" />
                </button>
              </span>
            ))}
            {chips.length ? (
              <Button color="primary" variant="text" size="small" onClick={clearAll}>{copyFor("action.clear_all", "Clear all")}</Button>
            ) : null}
          </div>
        }
      >
        <TextField
          select
          label={copyFor("filter.status", "Status")}
          value={status}
          onChange={({ target: { value: v } }) => { setStatus(v); setPage(1); }}
          sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {statusOptions.map((option) => (
            <MenuItem key={option.value} value={option.value}>
              {option.label}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          label={copyFor("filter.assignee", "Assignee")}
          value={assignee}
          onChange={({ target: { value: v } }) => { setAssignee(v); setPage(1); }}
          sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
          slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
        >
          {assigneeOptions.map((option) => (
            <MenuItem key={option.value} value={option.value}>
              {option.label}
            </MenuItem>
          ))}
        </TextField>
        <DateRangeField
          label={copyFor("filter.deadline", "Deadline")}
          from={from}
          to={to}
          fromLabel={copyFor("filter.deadline_from", "Deadline from")}
          toLabel={copyFor("filter.deadline_to", "Deadline to")}
          clearLabel={copyFor("filter.deadline_clear", "Clear deadline dates")}
          name="deadline"
          minWidth={230}
          className="lt-daterange"
          onChange={(next) => {
            setFrom(next.from);
            setTo(next.to);
            setPage(1);
          }}
        />
      </FilterBar>

      <div className={`tablewrap lt-tablewrap${dense ? " kit-dense" : ""}`} tabIndex={0} role="group" aria-label={copyFor("table.aria", "Leadership task progress")}>
        <Table data-enh="1" className="tbl lt-task-table">
          <TableHead>
            <TableRow>
              <TableCell component="th" className="lt-check-col">
                <Checkbox
                  checked={allShownSelected}
                  indeterminate={!allShownSelected && visible.some((t) => selection.includes(t.id))}
                  sx={{ p: { xs: 1.5, sm: 1 } }}
                  slotProps={{ input: { "aria-label": copyFor("action.select_all", "Select all rows on this page") } }}
                  onChange={(e) =>
                    setSelection((prev) =>
                      e.target.checked
                        ? Array.from(new Set([...prev, ...visible.map((t) => t.id)]))
                        : prev.filter((id) => !visible.some((t) => t.id === id)),
                    )
                  } />
              </TableCell>
              {TASK_COLUMNS.map((col) => {
                const active = sort.id === col.id;
                return (
                  <TableCell component="th"
                    key={col.id}
                    className={col.className}
                    aria-sort={active ? (sort.dir === "asc" ? "ascending" : "descending") : col.sortable ? "none" : undefined}
                  >
                    {col.sortable ? (
                      <TableSortLabel
                        hideSortIcon
                        active={active}
                        direction={active ? sort.dir : "asc"}
                        aria-label={`${col.label} — sort this page`}
                        onClick={() => toggleSort(col.id)}
                      >
                        {col.label}
                      </TableSortLabel>
                    ) : (
                      col.label
                    )}
                  </TableCell>
                );
              })}
              <TableCell component="th" className="lt-menu-col" aria-label={copyFor("action.more", "More actions")} />
            </TableRow>
          </TableHead>
          <TableBody>
            {visible.map((task) => (
              <TableRow key={task.id} className={task.id === selectedId ? "is-selected" : ""} aria-current={task.id === selectedId ? "true" : undefined}>
                <TableCell className="lt-check-col">
                  <Checkbox
                    checked={selection.includes(task.id)}
                    sx={{ p: { xs: 1.5, sm: 1 } }}
                    slotProps={{ input: { "aria-label": `${copyFor("action.select_row", "Select")} ${task.number}` } }}
                    onChange={(e) => setSelection((prev) => (e.target.checked ? [...prev, task.id] : prev.filter((id) => id !== task.id)))} />
                </TableCell>
                <TableCell className="lt-task-col">
                  <Link href={rowHref(task)} className="lt-tasklink" aria-label={`${copyFor("action.open_task", "Open task")} ${task.number}: ${task.title}`}>
                    <b>{task.number}</b>
                    <span className="muted small">{task.title}</span>
                  </Link>
                  {/* On a phone the table shows only its first column, so the same clock sits under
                      the title there (CSS shows one or the other, never both). */}
                  {task.deadlineTone ? <div className="lt-clock-inline">{renderClock(task, true)}</div> : null}
                </TableCell>
                <TableCell className="lt-days-col">{renderClock(task, true)}</TableCell>
                <TableCell className="lt-people-col">
                  <div className="lt-opname">
                    <span className="lt-avx">{initials(task.assignee)}</span>
                    <span>
                      {task.assignee}
                      <span className="lt-code muted">{task.assigneeRole}</span>
                    </span>
                  </div>
                </TableCell>
                <TableCell className="lt-raised-col lt-people-col">{task.raisedBy}</TableCell>
                <TableCell>{renderStatus(task)}</TableCell>
                <TableCell className="lt-evidence-col">
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 7, minWidth: 0 }}>
                    <Paperclip className="ic" style={{ width: 15, color: "var(--fg-muted)" }} aria-hidden="true" />
                    <b>{task.attachments}</b>
                    <span className="muted small">{task.evidence}</span>
                  </span>
                </TableCell>
                <TableCell className="lt-menu-col">
                  <RowMenu
                    ariaLabel={`${copyFor("action.more", "More actions")}: ${task.number}`}
                    actions={[
                      { label: copyFor("action.open_task", "Open task"), icon: <ExternalLink size={15} />, onSelect: () => { window.location.href = rowHref(task); } },
                      {
                        label: copyFor("action.copy_link", "Copy link"),
                        icon: <Copy size={15} />,
                        onSelect: () => {
                          void navigator.clipboard?.writeText(new URL(rowHref(task), window.location.origin).toString());
                        },
                      },
                    ]}
                  />
                </TableCell>
              </TableRow>
            ))}
            {visible.length === 0 ? (
              <TableRow>
                <TableCell colSpan={TASK_COLUMNS.length + 2}>
                  <div className="note">{copyFor("empty.no_match", "No tasks match these filters.")}</div>
                </TableCell>
              </TableRow>
            ) : null}
          </TableBody>
        </Table>
      </div>

      <TableFooter
        className="lt-tfoot"
        page={current}
        rowsPerPage={rows}
        total={total}
        rowsPerPageOptions={pageSizeOptions}
        onPageChange={setPage}
        onRowsPerPageChange={(n) => { setRows(n); setPage(1); }}
        left={
          selection.length ? (
            <span className="muted small">{selection.length} {copyFor("label.selected", "selected")}</span>
          ) : (
            <DenseToggle checked={dense} onChange={setDense} />
          )
        }
      />
    </>
  );
}

function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part[0]?.toUpperCase() ?? "")
    .join("") || "?";
}

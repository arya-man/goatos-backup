"use client";

import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { SegmentTabs } from "@/components/minimal/list/segment-tabs";
import { FileSpreadsheet, Plus, Search } from "lucide-react";

import { PageHeader } from "@/components/app/page-header";
import { Tag } from "@/components/ui-primitives";
import "./configuration-kit.css";
import { TablePaginationLinks } from "@/components/minimal/table/table-pagination-links";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { EmptyState } from "@/components/app/empty-state";
import Button from "@mui/material/Button";

/**
 * PRESENTATION-ONLY replica of the /configuration/items register view (rail · table card) for
 * Storybook and visual judging. `ItemsPage` itself pulls the Server Actions and the API client
 * into its module graph, which a browser bundle cannot carry; this component renders the same
 * classes and anatomy from plain props so the kit treatment can be seen and scored without the
 * backend. Keep its markup in step with `items-page.tsx`; it renders no copy of its own.
 */
export type RegisterPreviewGroup = { key: string; label: string; items: { key: string; label: string; count: number; active?: boolean }[] };
export type RegisterPreviewColumn = { key: string; label: string; numeric?: boolean; mono?: boolean };
export type RegisterPreviewRow = { id: string; cells: Record<string, string>; status: "active" | "archived"; builtin?: boolean; counts?: string };

export function RegisterPreview({
  copy,
  groups,
  title,
  total,
  columns,
  rows,
  status = "active",
  filters = [],
  page = 1,
}: {
  copy: Record<string, string>;
  groups: RegisterPreviewGroup[];
  title: string;
  total: number;
  columns: RegisterPreviewColumn[];
  rows: RegisterPreviewRow[];
  status?: "active" | "archived" | "all";
  filters?: { label: string; value: string }[];
  page?: number;
}) {
  const c = (key: string) => copy[key] ?? key;
  return (
    <div className="screen on cfg-items">
      <PageHeader title={c("title")} crumbs={[{ label: c("crumb") }, { label: title }]} />
      <div className="cfg-layout">
        <aside className="card cfg-rail" aria-label={c("rail.title")}>
          {groups.map((group) => (
            <div key={group.key} className="cfg-rail-group">
              <div className="cfg-rail-title">{group.label}</div>
              {group.items.map((item) => (
                <a key={item.key} href="#" className={item.active ? "cfg-rail-item on" : "cfg-rail-item"} aria-current={item.active ? "page" : undefined}>
                  <span>{item.label}</span>
                  <span className="cfg-rail-count">{item.count}</span>
                </a>
              ))}
            </div>
          ))}
        </aside>
        <section className="card cfg-main kit-tablecard" aria-label={title}>
          <div className="hd cfg-main-hd" style={{ flexWrap: "wrap" }}>
            <div>
              <h3>
                {title} <Tag tone="mut">{total}</Tag>
              </h3>
            </div>
            <div className="sp" style={{ flex: 1 }} />
            <a href="#" className="btn sm ghost">
              <FileSpreadsheet className="ic" style={{ width: 14 }} aria-hidden="true" />
              {c("sheet.title")}
            </a>
            <Button href="#" variant="contained" color="primary" size="small" startIcon={<Plus size={14} aria-hidden="true" />}>
              {c("action.create_row.label")} {title.toLowerCase()}
            </Button>
          </div>
          <div className="tbar">
            <form className="tsearch" role="search" onSubmit={(event) => event.preventDefault()}>
              <Search className="ic" aria-hidden="true" />
              <input name="q" placeholder={`${c("search.placeholder")} ${title.toLowerCase()}`} aria-label={c("search.placeholder")} />
            </form>
            {filters.map((filter) => (
              <TextField
                key={filter.label}
                select
                label={filter.label}
                value={filter.value}
                sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
                slotProps={{ inputLabel: { shrink: true } }}
              >
                <MenuItem value={filter.value}>{filter.value}</MenuItem>
              </TextField>
            ))}
            <div className="sp" style={{ flex: 1 }} />
            <SegmentTabs
              ariaLabel={c("column.status")}
              value={status}
              tabs={(["active", "archived", "all"] as const).map((key) => ({ value: key, label: c(`status.${key}`), href: "#" }))}
            />
          </div>
          <div className="bd">
            {rows.length === 0 ? (
              <EmptyState title={c("empty.rows")} />
            ) : (
              <div className="tablewrap" tabIndex={0} role="group" aria-label={title}>
                <Table className="tbl">
                  <TableHead>
                    <TableRow>
                      <TableCell component="th">{c("column.display")}</TableCell>
                      {columns.map((column) => (
                        <TableCell component="th" key={column.key}>{column.label}</TableCell>
                      ))}
                      {rows.some((row) => row.counts) ? (
                        <TableCell component="th">{c("column.counts")}</TableCell>
                      ) : null}
                      <TableCell component="th">{c("column.status")}</TableCell>
                    </TableRow>
                  </TableHead>
                  <TableBody>
                    {rows.map((row) => (
                      <TableRow key={row.id} className={row.status === "archived" ? "cfg-archived" : undefined}>
                        <TableCell>
                          <a href="#" className="cfg-row-link">
                            <b>{row.cells.name ?? row.id}</b>
                          </a>
                          {row.builtin ? (
                            <span className="muted small" style={{ marginLeft: 8 }}>
                              {c("tag.builtin")}
                            </span>
                          ) : null}
                        </TableCell>
                        {columns.map((column) => (
                          <TableCell key={column.key} className={column.numeric ? "num" : undefined}>
                            {column.mono ? <span className="mono muted">{row.cells[column.key] ?? "—"}</span> : (row.cells[column.key] ?? "—")}
                          </TableCell>
                        ))}
                        {rows.some((r) => r.counts) ? <TableCell className="muted small">{row.counts ?? "—"}</TableCell> : null}
                        <TableCell>
                          <Tag tone={row.status === "active" ? "ok" : "mut"}>{c(`status.${row.status}`)}</Tag>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
            <TablePaginationLinks
              className="pmx-pager"
              page={0}
              rowsPerPage={Math.max(rows.length, 1)}
              count={-1}
              rangeLabel={rows.length === 0 ? "0" : `1–${rows.length} ${c("pager.of")} ${total}`}
              prevLabel={c("action.previous")}
              nextLabel={c("action.next")}
            />
          </div>
        </section>
      </div>
    </div>
  );
}

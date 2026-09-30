"use client";

import { PageRoot } from "@/components/app/page-root";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Grid from "@mui/material/Grid";
import Table from "@mui/material/Table";
import Button from "@mui/material/Button";
import MenuItem from "@mui/material/MenuItem";
import TableRow from "@mui/material/TableRow";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TextField from "@mui/material/TextField";
import CardHeader from "@mui/material/CardHeader";
import Typography from "@mui/material/Typography";
import InputAdornment from "@mui/material/InputAdornment";

import { PageHeader } from "@/components/app/page-header";
import { EmptyState } from "@/components/app/empty-state";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TemplateTabs } from "@/components/app/template-tabs";
import { TableHeadCustom, TablePaginationLinks } from "@/components/app/table";
import { OrderTableToolbar } from "@/components/app/sections/order/order-table-toolbar";
import { orderToolbarFilterSx, orderToolbarSearchSx } from "@/components/app/order-toolbar-filter";
import { MailNavItem } from "@/components/app/sections/mail/mail-nav-item";
import { TAP_MIN } from "@/theme/tap-target";

/**
 * PRESENTATION-ONLY replica of the /configuration/items register view (rail · table card) for
 * Storybook and visual judging. `ItemsPage` itself pulls the Server Actions and the API client
 * into its module graph, which a browser bundle cannot carry; this component renders the same
 * template anatomy (mail nav rail + user-list table card) from plain props so the kit treatment can be seen and scored without the
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
  const hasCounts = rows.some((row) => row.counts);
  return (
    <PageRoot>
      <PageHeader
        title={c("title")}
        crumbs={[{ label: c("crumb") }, { label: title }]}
        actions={
          <Button href="#" variant="contained" color="primary" startIcon={<Iconify icon="mingcute:add-line" />}>
            {c("action.create_row.label")} {title.toLowerCase()}
          </Button>
        }
      />
      <Grid container spacing={3}>
        <Grid size={{ xs: 12, md: 4, lg: 3 }}>
          <Card component="aside" aria-label={c("rail.title")} sx={{ py: 1.5 }}>
            <Box component="nav">
              {groups.map((group) => (
                <Box key={group.key} component="ul" sx={{ m: 0, px: 1.5, pb: 1, listStyle: "none", display: { xs: "flex", md: "block" }, flexWrap: "wrap", columnGap: 0.5 }}>
                  <Typography component="li" variant="overline" sx={{ width: 1, display: "block", px: 1, pt: 1.5, pb: 1, color: "text.disabled" }}>
                    {group.label}
                  </Typography>
                  {group.items.map((item) => (
                    <MailNavItem key={item.key} selected={!!item.active} href="#" label={{ name: item.label, count: item.count }} slotProps={{ button: { minHeight: TAP_MIN }, label: { textTransform: "none" } }} />
                  ))}
                </Box>
              ))}
            </Box>
          </Card>
        </Grid>
        <Grid size={{ xs: 12, md: 8, lg: 9 }}>
          <Card component="section" aria-label={title}>
            <CardHeader
              title={title}
              action={
                <Button href="#" size="small" color="inherit" variant="outlined" startIcon={<Iconify icon="solar:file-text-bold" />}>
                  {c("sheet.title")}
                </Button>
              }
            />
            <TemplateTabs
              ariaLabel={c("column.status")}
              value={status}
              sx={{ px: { md: 2.5 }, mt: 1 }}
              items={(["active", "archived", "all"] as const).map((key) => ({ value: key, label: c(`status.${key}`), count: key === status ? total : undefined, href: "#" }))}
            />
            <OrderTableToolbar
              filters={filters.map((filter) => (
                <Box key={filter.label} sx={orderToolbarFilterSx}>
                  <TextField select fullWidth label={filter.label} value={filter.value} slotProps={{ inputLabel: { shrink: true } }}>
                    <MenuItem value={filter.value}>{filter.value}</MenuItem>
                  </TextField>
                </Box>
              ))}
              search={<Box sx={orderToolbarSearchSx}><form role="search" onSubmit={(event) => event.preventDefault()}>
                  <TextField
                    fullWidth
                    name="q"
                    placeholder={`${c("search.placeholder")} ${title.toLowerCase()}`}
                    slotProps={{
                      htmlInput: { "aria-label": c("search.placeholder") },
                      input: {
                        startAdornment: (
                          <InputAdornment position="start">
                            <Iconify icon="eva:search-fill" sx={{ color: "text.disabled" }} />
                          </InputAdornment>
                        ),
                      },
                    }}
                  />
                </form></Box>}
            />
            {rows.length === 0 ? (
              <EmptyState title={c("empty.rows")} />
            ) : (
              <Scrollbar>
                <Box tabIndex={0} role="group" aria-label={title}>
                  <Table sx={{ minWidth: 720 }}>
                    <TableHeadCustom
                      headCells={[
                        { id: "display", label: c("column.display") },
                        ...columns.map((column) => ({ id: column.key, label: column.label, align: column.numeric ? ("right" as const) : undefined })),
                        ...(hasCounts ? [{ id: "counts", label: c("column.counts") }] : []),
                        { id: "status", label: c("column.status") },
                      ]}
                    />
                    <TableBody>
                      {rows.map((row) => (
                        <TableRow key={row.id} hover>
                          <TableCell>
                            <a href="#" className="config-row-link">
                              <Typography component="span" variant="subtitle2">
                                {row.cells.name ?? row.id}
                              </Typography>
                            </a>
                            {row.builtin ? (
                              <Label variant="soft" sx={{ ml: 1 }}>
                                {c("tag.builtin")}
                              </Label>
                            ) : null}
                          </TableCell>
                          {columns.map((column) => (
                            <TableCell key={column.key} align={column.numeric ? "right" : "left"} sx={column.mono ? { color: "text.secondary" } : undefined}>
                              {row.cells[column.key] ?? "—"}
                            </TableCell>
                          ))}
                          {hasCounts ? <TableCell sx={{ color: "text.secondary" }}>{row.counts ?? "—"}</TableCell> : null}
                          <TableCell>
                            <Label variant="soft" color={row.status === "active" ? "success" : "default"}>
                              {c(`status.${row.status}`)}
                            </Label>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </Box>
              </Scrollbar>
            )}
            <TablePaginationLinks
              page={Math.max(0, page - 1)}
              rowsPerPage={Math.max(rows.length, 1)}
              count={-1}
              rangeLabel={rows.length === 0 ? "0" : `1–${rows.length} ${c("pager.of")} ${total}`}
              prevLabel={c("action.previous")}
              nextLabel={c("action.next")}
            />
          </Card>
        </Grid>
      </Grid>
    </PageRoot>
  );
}

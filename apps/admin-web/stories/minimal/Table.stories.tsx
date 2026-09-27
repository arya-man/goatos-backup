import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within, waitFor } from "storybook/test";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Chip from "@mui/material/Chip";
import Table from "@mui/material/Table";
import Button from "@mui/material/Button";
import Tooltip from "@mui/material/Tooltip";
import TableRow from "@mui/material/TableRow";
import Checkbox from "@mui/material/Checkbox";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import IconButton from "@mui/material/IconButton";
import { Label } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { ConfirmDialog } from "@/components/minimal/custom-dialog";
import { LabelTabs, ListToolbar } from "@/components/app/list";
import { chipProps, FiltersBlock, FiltersResult } from "@/components/minimal/filters-result";
import {
  useTable,
  emptyRows,
  rowInPage,
  getComparator,
  TableNoData,
  TableSkeleton,
  TableEmptyRows,
  TableHeadCustom,
  TableSelectedAction,
  TablePaginationCustom,
  type TableHeadCellProps,
} from "@/components/app/table";
import { GOATS, type GoatRow } from "./_goats";
import { withMinimalTheme, mobile } from "./_minimal";

const HEAD: TableHeadCellProps[] = [
  { id: "tag", label: "Tag" },
  { id: "pen", label: "Pen", width: 140 },
  { id: "breed", label: "Breed", width: 160 },
  { id: "weightKg", label: "Weight (kg)", width: 130, align: "right" },
  { id: "status", label: "Status", width: 110 },
  { id: "", width: 64 },
];

const STATUS_COLOR = { active: "success", sick: "warning", sold: "default" } as const;

type Filters = { name: string; pens: string[]; status: string };

function GoatList({ rows = GOATS, loading = false, initial }: { rows?: GoatRow[]; loading?: boolean; initial?: Partial<Filters> }) {
  const table = useTable({ defaultOrderBy: "tag" });
  const [filters, setFilters] = React.useState<Filters>({ name: "", pens: [], status: "all", ...initial });
  const [confirm, setConfirm] = React.useState(false);
  const update = (patch: Partial<Filters>) => {
    table.onResetPage();
    setFilters((f) => ({ ...f, ...patch }));
  };

  const filtered = rows
    .filter((r) => filters.status === "all" || r.status === filters.status)
    .filter((r) => !filters.pens.length || filters.pens.includes(r.pen))
    .filter((r) => !filters.name || r.tag.toLowerCase().includes(filters.name.toLowerCase()))
    .sort(getComparator(table.order, table.orderBy as keyof GoatRow));
  const canReset = !!filters.name || filters.pens.length > 0 || filters.status !== "all";
  const notFound = !filtered.length;
  const count = (s: string) => rows.filter((r) => s === "all" || r.status === s).length;

  return (
    <Card>
      <LabelTabs
        value={filters.status}
        onChange={(status) => update({ status })}
        tabs={[
          { value: "all", label: "All", count: count("all") },
          { value: "active", label: "Active", count: count("active"), color: "success" },
          { value: "sick", label: "Sick", count: count("sick"), color: "warning" },
          { value: "sold", label: "Sold", count: count("sold") },
        ]}
      />
      <ListToolbar
        search={filters.name}
        onSearch={(name) => update({ name })}
        searchPlaceholder="Search tag..."
        select={{ label: "Pen", options: ["P-01", "P-02", "P-03", "Kid shed", "Isolation"].map((p) => ({ value: p, label: p })), value: filters.pens, onChange: (pens) => update({ pens }) }}
        actions={[
          { label: "Print", icon: "solar:printer-minimalistic-bold", onClick: () => undefined },
          { label: "Export", icon: "solar:export-bold", onClick: () => undefined },
        ]}
      />
      {canReset && (
        <FiltersResult totalResults={filtered.length} onReset={() => update({ name: "", pens: [], status: "all" })} sx={{ p: 2.5, pt: 0 }}>
          <FiltersBlock label="Status:" isShow={filters.status !== "all"}>
            <Chip {...chipProps} label={filters.status} onDelete={() => update({ status: "all" })} sx={{ textTransform: "capitalize" }} />
          </FiltersBlock>
          <FiltersBlock label="Pen:" isShow={!!filters.pens.length}>
            {filters.pens.map((p) => (
              <Chip {...chipProps} key={p} label={p} onDelete={() => update({ pens: filters.pens.filter((x) => x !== p) })} />
            ))}
          </FiltersBlock>
          <FiltersBlock label="Keyword:" isShow={!!filters.name}>
            <Chip {...chipProps} label={filters.name} onDelete={() => update({ name: "" })} />
          </FiltersBlock>
        </FiltersResult>
      )}
      <Box sx={{ position: "relative" }}>
        <TableSelectedAction
          dense={table.dense}
          numSelected={table.selected.length}
          rowCount={filtered.length}
          onSelectAllRows={(checked) => table.onSelectAllRows(checked, filtered.map((r) => r.id))}
          action={
            <Tooltip title="Delete">
              <IconButton aria-label="Delete selected" color="primary" onClick={() => setConfirm(true)}>
                <Iconify icon="solar:trash-bin-trash-bold" />
              </IconButton>
            </Tooltip>
          }
        />
        <Scrollbar>
          <Table size={table.dense ? "small" : "medium"} sx={{ minWidth: 720 }}>
            <TableHeadCustom
              order={table.order}
              orderBy={table.orderBy}
              headCells={HEAD}
              rowCount={filtered.length}
              numSelected={table.selected.length}
              onSort={table.onSort}
              onSelectAllRows={(checked) => table.onSelectAllRows(checked, filtered.map((r) => r.id))}
            />
            <TableBody>
              {loading ? (
                <TableSkeleton rowCount={table.rowsPerPage} cellCount={HEAD.length + 1} sx={{ height: 69 }} />
              ) : (
                <>
                  {rowInPage(filtered, table.page, table.rowsPerPage).map((row) => (
                    <TableRow hover key={row.id} selected={table.selected.includes(row.id)}>
                      <TableCell padding="checkbox">
                        <Checkbox checked={table.selected.includes(row.id)} onClick={() => table.onSelectRow(row.id)} slotProps={{ input: { "aria-label": `Select ${row.tag}` } }} />
                      </TableCell>
                      <TableCell sx={{ fontWeight: 600 }}>{row.tag}</TableCell>
                      <TableCell>{row.pen}</TableCell>
                      <TableCell>{row.breed}</TableCell>
                      <TableCell align="right">{row.weightKg.toFixed(1)}</TableCell>
                      <TableCell>
                        <Label variant="soft" color={STATUS_COLOR[row.status]}>{row.status}</Label>
                      </TableCell>
                      <TableCell align="right">
                        <IconButton aria-label={`Edit ${row.tag}`}>
                          <Iconify icon="solar:pen-bold" />
                        </IconButton>
                      </TableCell>
                    </TableRow>
                  ))}
                  <TableEmptyRows height={table.dense ? 56 : 76} emptyRows={emptyRows(table.page, table.rowsPerPage, filtered.length)} />
                  <TableNoData notFound={notFound} />
                </>
              )}
            </TableBody>
          </Table>
        </Scrollbar>
      </Box>
      <TablePaginationCustom
        page={table.page}
        dense={table.dense}
        count={filtered.length}
        rowsPerPage={table.rowsPerPage}
        onPageChange={table.onChangePage}
        onChangeDense={table.onChangeDense}
        onRowsPerPageChange={table.onChangeRowsPerPage}
      />
      <ConfirmDialog
        open={confirm}
        onClose={() => setConfirm(false)}
        title="Delete"
        content={<>Delete <strong>{table.selected.length}</strong> goats?</>}
        action={<Button variant="contained" color="error" onClick={() => setConfirm(false)}>Delete</Button>}
      />
    </Card>
  );
}

const meta: Meta = { title: "Minimal/Table", decorators: [withMinimalTheme] };
export default meta;
type Story = StoryObj;

export const ListView: Story = {
  render: () => <GoatList />,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    await userEvent.click(c.getByRole("tab", { name: /Sick/ }));
    await waitFor(() => expect(c.getByRole("tab", { name: /Sick/ })).toHaveAttribute("aria-selected", "true"));
    await userEvent.click(c.getByRole("tab", { name: /All/ }));
  },
};
export const Filtered: Story = { render: () => <GoatList initial={{ name: "MSG-010", pens: ["P-01", "P-02"], status: "active" }} /> };
export const Loading: Story = { render: () => <GoatList loading /> };
export const NoData: Story = { render: () => <GoatList rows={[]} /> };
export const ListViewMobile: Story = { render: () => <GoatList />, globals: mobile };
export const FilteredMobile: Story = { render: () => <GoatList initial={{ name: "MSG", pens: ["P-01"], status: "active" }} />, globals: mobile };

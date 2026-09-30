import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { Iconify } from "@/components/minimal/iconify";
import { DenseToggle } from "@/components/app/dense-toggle";
import { RowMenu } from "@/components/app/row-menu";
import { TableSkeleton } from "@/components/app/skeletons";
import { TableFooter } from "@/components/app/table-footer";
import Box from "@mui/material/Box";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableContainer from "@mui/material/TableContainer";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import { Label } from "@/components/minimal/label";
import { Frame, MOBILE, PENS, StateBlock, States, VENDORS } from "./_fixtures";
import { EmptyState } from "@/components/app/empty-state";

/**
 * A Goat OS table on the template: MUI `Card` + `CardHeader` + `TableContainer > Table`
 * (`TableHead` / `TableBody` / `TableRow` / `TableCell`) + `TableFooter`. The legacy
 * `.kit-tablecard` / `.tablewrap` / `.tbl` classes are deleted; these stories pin the template
 * treatment so it can be visually regressed.
 */
const meta = {
  title: "Kit/Tables/DataTable",
  parameters: { docs: { description: { component: "The kit table treatment: Card shell, MUI Table rows, dense mode, row menu, footer paging." } } },
  decorators: [(S: () => React.ReactElement) => <Frame width={1040}>{S()}</Frame>],
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

const COLS = ["Pen", "Park", "Head", "ADG (g/day)", "Kids", "Vet", "Status"];

function StatusChip({ status }: { status: string }) {
  const color = status === "Healthy" ? "success" : status === "Watch" ? "warning" : "info";
  return <Label color={color}>{status}</Label>;
}

function PenTable({ rows = PENS, dense = false, withMenu = true }: { rows?: typeof PENS; dense?: boolean; withMenu?: boolean }) {
  return (
    <TableContainer data-dense={dense ? "" : undefined} tabIndex={0} role="group" aria-label="Pens">
      <Table size={dense ? "small" : "medium"}>
        <TableHead>
          <TableRow>
            {COLS.map((c) => <TableCell key={c}>{c}</TableCell>)}
            {withMenu ? <TableCell aria-label="Row actions" /> : null}
          </TableRow>
        </TableHead>
        <TableBody>
          {rows.map((r) => (
            <TableRow key={r.pen} hover>
              <TableCell>{r.pen}</TableCell>
              <TableCell>{r.park}</TableCell>
              <TableCell>{r.head}</TableCell>
              <TableCell>{r.adg}</TableCell>
              <TableCell>{r.kids}</TableCell>
              <TableCell>{r.vet}</TableCell>
              <TableCell><StatusChip status={r.status} /></TableCell>
              {withMenu ? (
                <TableCell align="right">
                  <RowMenu
                    ariaLabel={`Actions for ${r.pen}`}
                    actions={[
                      { label: "Edit pen", icon: <Iconify icon="solar:pen-bold" width={15} />, onSelect: () => {} },
                      { label: "Open weighing", icon: <Iconify icon="solar:bill-list-bold-duotone" width={15} />, onSelect: () => {} },
                      { label: "Archive pen", icon: <Iconify icon="solar:trash-bin-trash-bold" width={15} />, danger: true, onSelect: () => {} },
                    ]}
                  />
                </TableCell>
              ) : null}
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

function Shell({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <Card aria-label="Pen register">
      <CardHeader
        sx={{ alignItems: "center", gap: 1.5, flexWrap: "wrap", [`& .${cardHeaderClasses.action}`]: { m: 0, flex: { xs: "1 1 100%", sm: "0 0 auto" }, minWidth: 0, maxWidth: "100%" } }}
        title={<Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 1 }}><Iconify icon="solar:bill-list-bold-duotone" width={18} sx={{ color: "primary.main" }} />Pen register</Box>}
        subheader="Kranji, Lim Chu Kang, Sungei Tengah and Mandai"
        action={action}
      />
      <Box sx={{ mt: 1.5 }}>{children}</Box>
    </Card>
  );
}

export const Default: Story = {
  render: function Render() {
    const [page, setPage] = React.useState(1);
    const [rpp, setRpp] = React.useState(10);
    return (
      <Shell action={<Typography variant="body2" sx={{ color: "success.main" }}>+3.4% head vs last week</Typography>}>
        <PenTable />
        <TableFooter page={page} rowsPerPage={rpp} total={1248} onPageChange={setPage} onRowsPerPageChange={(n) => { setRpp(n); setPage(1); }} />
      </Shell>
    );
  },
};

export const Dense: Story = {
  render: function Render() {
    const [dense, setDense] = React.useState(true);
    return (
      <Shell>
        <PenTable dense={dense} />
        <TableFooter page={1} rowsPerPage={10} total={1248} onPageChange={() => {}} onRowsPerPageChange={() => {}} left={<DenseToggle checked={dense} onChange={setDense} />} />
      </Shell>
    );
  },
};

export const LoadingSkeleton: Story = {
  render: () => (
    <Shell>
      <TableSkeleton columns={7} rows={8} header={false} bare />
    </Shell>
  ),
};

export const Empty: Story = {
  render: () => (
    <Shell>
      <EmptyState filled icon={<Iconify icon="solar:bill-list-bold-duotone" />} title="No pens match this filter. Clear the park filter or add a pen." />
      <TableFooter page={1} rowsPerPage={25} total={0} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
    </Shell>
  ),
};

export const ErrorState: Story = {
  render: () => (
    <Shell>
      <Box sx={{ display: "grid", justifyItems: "center", gap: 1.25, px: 3, py: 7, textAlign: "center" }}>
        <Iconify icon="solar:danger-triangle-bold" sx={{ color: "error.main" }} />
        <Typography variant="body2" sx={{ color: "text.secondary" }}>Could not load the pen register (weighing service timed out).</Typography>
        <Button type="button" variant="outlined" color="inherit" size="small">Retry</Button>
      </Box>
    </Shell>
  ),
};

export const SingleRow: Story = { render: () => <Shell><PenTable rows={PENS.slice(0, 1)} /></Shell> };

export const ManyRowsAndOverflow: Story = {
  render: () => (
    <States>
      <StateBlock label="Long vendor names + 40 rows — horizontal scroll inside the TableContainer">
        <Card>
          <TableContainer tabIndex={0} role="group" aria-label="Vendor loads">
            <Table>
              <TableHead><TableRow><TableCell>Vendor</TableCell><TableCell>Park</TableCell><TableCell>Load ref</TableCell><TableCell>Head</TableCell><TableCell>Avg weight</TableCell><TableCell>Status</TableCell></TableRow></TableHead>
              <TableBody>
                {Array.from({ length: 40 }, (_, i) => {
                  const v = VENDORS[i % VENDORS.length];
                  const p = PENS[i % PENS.length];
                  return (
                    <TableRow key={i} hover>
                      <TableCell sx={{ whiteSpace: "nowrap" }}>{v}</TableCell>
                      <TableCell>{p.park}</TableCell>
                      <TableCell>LOAD-2026-{String(4100 + i)}</TableCell>
                      <TableCell>{40 + (i % 17)}</TableCell>
                      <TableCell>{(22 + (i % 9) * 0.4).toFixed(1)} kg</TableCell>
                      <TableCell><StatusChip status={i % 3 === 0 ? "Watch" : "Healthy"} /></TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </TableContainer>
          <TableFooter page={1} rowsPerPage={50} total={2140} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
        </Card>
      </StateBlock>
    </States>
  ),
};

/** Behaviour: the row menu opens, Escape closes it. */
export const RowMenuBehaviour: Story = {
  render: () => <Shell><PenTable rows={PENS.slice(0, 3)} /></Shell>,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const trigger = c.getByLabelText("Actions for Pen A-12");
    await expect(trigger).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(trigger);
    await expect(trigger).toHaveAttribute("aria-expanded", "true");
    const item = await within(document.body).findByText("Archive pen");
    // waitFor: the pop surface animates in, so it is in the DOM one frame before it is visible.
    await waitFor(() => expect(item).toBeVisible());
    await userEvent.keyboard("{Escape}");
    await expect(trigger).toHaveAttribute("aria-expanded", "false");
  },
};

export const Mobile: Story = {
  globals: MOBILE,
  render: function Render() {
    const [page, setPage] = React.useState(2);
    return (
      <Shell>
        <PenTable rows={PENS.slice(0, 6)} />
        <TableFooter page={page} rowsPerPage={10} total={1248} onPageChange={setPage} onRowsPerPageChange={() => {}} />
      </Shell>
    );
  },
};

export const MobileEmpty: Story = {
  globals: MOBILE,
  render: () => (
    <Shell>
      <EmptyState filled icon={<Iconify icon="solar:bill-list-bold-duotone" />} title="No pens in Mandai Quarantine today." />
    </Shell>
  ),
};

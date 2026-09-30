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
import { cx, toneVars, type KitTone } from "@/lib/tone";
import { Frame, MOBILE, PENS, StateBlock, States, VENDORS } from "./_fixtures";
import { EmptyState } from "@/components/app/empty-state";

/**
 * There is no `DataTable` component in the kit — a Goat OS table is a composition:
 * MUI `Card className="kit-tablecard"` (no padding) + MUI `CardHeader` + `div.tablewrap > table.tbl`
 * + `TableFooter`. These stories pin that treatment so it can be visually regressed.
 */
const meta = {
  title: "Kit/Tables/DataTable",
  parameters: { docs: { description: { component: "The kit table treatment: tablecard shell, .tbl rows, dense mode, row menu, footer paging." } } },
  decorators: [(S: () => React.ReactElement) => <Frame width={1040}>{S()}</Frame>],
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

const COLS = ["Pen", "Park", "Head", "ADG (g/day)", "Kids", "Vet", "Status"];

function StatusChip({ status }: { status: string }) {
  const tone: KitTone = status === "Healthy" ? "success" : status === "Watch" ? "warning" : "info";
  const t = toneVars(tone);
  return (
    <span style={{ display: "inline-flex", alignItems: "center", background: t.soft, color: t.ink, borderRadius: 999, padding: "3px 10px", fontSize: 12, fontWeight: 700, whiteSpace: "nowrap" }}>
      {status}
    </span>
  );
}

function PenTable({ rows = PENS, dense = false, withMenu = true }: { rows?: typeof PENS; dense?: boolean; withMenu?: boolean }) {
  return (
    <div data-dense={dense ? "" : undefined} tabIndex={0} role="group" aria-label="Pens">
      <table className="tbl">
        <thead>
          <tr>
            {COLS.map((c) => <th key={c}>{c}</th>)}
            {withMenu ? <th aria-label="Row actions" /> : null}
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.pen}>
              <td>{r.pen}</td>
              <td>{r.park}</td>
              <td>{r.head}</td>
              <td>{r.adg}</td>
              <td>{r.kids}</td>
              <td>{r.vet}</td>
              <td><StatusChip status={r.status} /></td>
              {withMenu ? (
                <td style={{ textAlign: "right" }}>
                  <RowMenu
                    ariaLabel={`Actions for ${r.pen}`}
                    actions={[
                      { label: "Edit pen", icon: <Iconify icon="solar:pen-bold" width={15} />, onSelect: () => {} },
                      { label: "Open weighing", icon: <Iconify icon="solar:bill-list-bold-duotone" width={15} />, onSelect: () => {} },
                      { label: "Archive pen", icon: <Iconify icon="solar:trash-bin-trash-bold" width={15} />, danger: true, onSelect: () => {} },
                    ]}
                  />
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Shell({ children, action }: { children: React.ReactNode; action?: React.ReactNode }) {
  return (
    <Card className="kit-tablecard" aria-label="Pen register">
      <CardHeader
        sx={{ [`& .${cardHeaderClasses.action}`]: { m: 0, flex: { xs: "1 1 100%", sm: "0 0 auto" }, minWidth: 0, maxWidth: "100%" } }}
        style={{ padding: "20px 24px 12px", alignItems: "center", gap: 12, flexWrap: "wrap" }}
        title={<span style={{ display: "inline-flex", alignItems: "center", gap: 8 }}><Iconify icon="solar:bill-list-bold-duotone" style={{ width: 18, color: "var(--primary)" }} />Pen register</span>}
        subheader="Kranji, Lim Chu Kang, Sungei Tengah and Mandai"
        action={action}
      />
      {children}
    </Card>
  );
}

export const Default: Story = {
  render: function Render() {
    const [page, setPage] = React.useState(1);
    const [rpp, setRpp] = React.useState(10);
    return (
      <Shell action={<span style={{ fontSize: 13, color: "var(--success-ink)" }}>+3.4% head vs last week</span>}>
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
      <div style={{ display: "grid", justifyItems: "center", gap: 10, padding: "56px 24px", textAlign: "center" }}>
        <Iconify icon="solar:danger-triangle-bold" style={{ color: "var(--error)" }} />
        <div className="small">Could not load the pen register (weighing service timed out).</div>
        <Button type="button" variant="outlined" color="inherit" size="small">Retry</Button>
      </div>
    </Shell>
  ),
};

export const SingleRow: Story = { render: () => <Shell><PenTable rows={PENS.slice(0, 1)} /></Shell> };

export const ManyRowsAndOverflow: Story = {
  render: () => (
    <States>
      <StateBlock label="Long vendor names + 40 rows — horizontal scroll inside .tablewrap">
        <Card className="kit-tablecard">
          <div className="tablewrap" tabIndex={0} role="group" aria-label="Vendor loads">
            <table className="tbl">
              <thead><tr><th>Vendor</th><th>Park</th><th>Load ref</th><th>Head</th><th>Avg weight</th><th>Status</th></tr></thead>
              <tbody>
                {Array.from({ length: 40 }, (_, i) => {
                  const v = VENDORS[i % VENDORS.length];
                  const p = PENS[i % PENS.length];
                  return (
                    <tr key={i}>
                      <td>{v}</td>
                      <td>{p.park}</td>
                      <td>LOAD-2026-{String(4100 + i)}</td>
                      <td>{40 + (i % 17)}</td>
                      <td>{(22 + (i % 9) * 0.4).toFixed(1)} kg</td>
                      <td><StatusChip status={i % 3 === 0 ? "Watch" : "Healthy"} /></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
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

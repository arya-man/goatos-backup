import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { RowMenu } from "@/components/app/row-menu";
import { TableFooter } from "@/components/app/table-footer";
import { Frame, Labelled, mobile } from "../_fixtures/frame";
import { penRows } from "../_fixtures/goatos";

function Footer(props: Partial<React.ComponentProps<typeof TableFooter>>) {
  const [page, setPage] = React.useState(props.page ?? 1);
  const [rows, setRows] = React.useState(props.rowsPerPage ?? 10);
  return (
    <TableFooter
      total={penRows.length}
      onRowsPerPageChange={(n) => {
        setRows(n);
        setPage(1);
      }}
      {...props}
      page={page}
      rowsPerPage={rows}
      onPageChange={setPage}
    />
  );
}

const meta: Meta<typeof TableFooter> = {
  title: "Kit/Pagination/TableFooter states",
  component: TableFooter,
  parameters: { layout: "fullscreen", dualTheme: { height: 420 } },
  decorators: [
    (Story) => (
      <Frame>
        <Story />
      </Frame>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof TableFooter>;

export const Default: Story = { render: () => <Card sx={{ p: 2 }}><Footer /></Card> };

export const States: Story = {
  render: () => (
    <div style={{ display: "grid", gap: 20 }}>
      <Labelled label="First page — prev disabled"><Card sx={{ p: 2 }}><Footer page={1} /></Card></Labelled>
      <Labelled label="Middle page"><Card sx={{ p: 2 }}><Footer page={7} /></Card></Labelled>
      <Labelled label="Last page — next disabled, short range"><Card sx={{ p: 2 }}><Footer page={14} /></Card></Labelled>
      <Labelled label="Empty table — 0 of 0, both arrows disabled"><Card sx={{ p: 2 }}><Footer total={0} /></Card></Labelled>
      <Labelled label="Single page — both arrows disabled"><Card sx={{ p: 2 }}><Footer total={6} /></Card></Labelled>
      <Labelled label="Exactly one full page"><Card sx={{ p: 2 }}><Footer total={10} /></Card></Labelled>
      <Labelled label="No rows-per-page control"><Card sx={{ p: 2 }}><TableFooter page={2} rowsPerPage={25} total={137} onPageChange={() => {}} /></Card></Labelled>
      <Labelled label="Custom rows-per-page options"><Card sx={{ p: 2 }}><Footer rowsPerPage={5} rowsPerPageOptions={[5, 15, 30]} /></Card></Labelled>
    </div>
  ),
};

/** The `left` slot: selection count, a dense toggle, or a row menu. */
export const WithLeftSlot: Story = {
  render: () => (
    <div style={{ display: "grid", gap: 20 }}>
      <Labelled label="Selection count">
        <Card sx={{ p: 2 }}><Footer left={<span style={{ fontSize: 13, color: "var(--fg-muted)" }}>14 head selected</span>} /></Card>
      </Labelled>
      <Labelled label="Long left slot (overflow)">
        <Card sx={{ p: 2 }}>
          <Footer left={<span style={{ fontSize: 13, color: "var(--fg-muted)" }}>14 head selected across Godel 1 - Part 1, Godel 2 - Part 3 and Quarantine pen A</span>} />
        </Card>
      </Labelled>
      <Labelled label="Bulk action menu">
        <Card sx={{ p: 2 }}>
          <Footer left={<RowMenu ariaLabel="Bulk actions" align="left" actions={[{ label: "Export selection", onSelect: () => {} }, { label: "Move to cull list", onSelect: () => {}, danger: true }]} />} />
        </Card>
      </Labelled>
    </div>
  ),
};

/** Huge totals: the range text must stay on tabular numbers and not wrap. */
export const LargeTotals: Story = {
  render: () => (
    <Card sx={{ p: 2 }}>
      <Footer page={842} rowsPerPage={100} total={1_284_902} />
    </Card>
  ),
};

/** Under a real table, which is where it always sits. */
export const UnderTable: Story = {
  render: () => {
    function Paged() {
      const [page, setPage] = React.useState(1);
      const [rows, setRows] = React.useState(10);
      const slice = penRows.slice((page - 1) * rows, page * rows);
      return (
        <Card sx={{ p: 2 }}>
          <div className="tablewrap" style={{ overflowX: "auto" }}>
            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13, minWidth: 640 }}>
              <thead>
                <tr style={{ textAlign: "left", color: "var(--fg-muted)" }}>
                  {["Tag", "Pen", "Vendor", "Weight", "ADG", "Status"].map((h) => (
                    <th key={h} style={{ padding: "8px 6px", borderBottom: "1px solid var(--line)", whiteSpace: "nowrap" }}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {slice.map((r) => (
                  <tr key={r.tag}>
                    <td style={{ padding: "8px 6px", borderBottom: "1px solid var(--line2)" }}>{r.tag}</td>
                    <td style={{ padding: "8px 6px", borderBottom: "1px solid var(--line2)" }}>{r.shed}</td>
                    <td style={{ padding: "8px 6px", borderBottom: "1px solid var(--line2)" }}>{r.vendor}</td>
                    <td style={{ padding: "8px 6px", borderBottom: "1px solid var(--line2)" }}>{r.weightKg} kg</td>
                    <td style={{ padding: "8px 6px", borderBottom: "1px solid var(--line2)" }}>{r.adg} g</td>
                    <td style={{ padding: "8px 6px", borderBottom: "1px solid var(--line2)" }}>{r.status}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <TableFooter page={page} rowsPerPage={rows} total={penRows.length} onPageChange={setPage} onRowsPerPageChange={(n) => { setRows(n); setPage(1); }} />
        </Card>
      );
    }
    return <Paged />;
  },
  parameters: { dualTheme: { height: 760 } },
};

export const MobileDefault: Story = { ...Default, globals: mobile };
export const MobileStates: Story = { ...States, globals: mobile, parameters: { dualTheme: { height: 1000 } } };
export const MobileUnderTable: Story = { ...UnderTable, globals: mobile, parameters: { dualTheme: { height: 760 } } };

/* ------------------------------------------------------------------ play -- */

/** The page arrows must move the range text, and cap at both ends. */
export const PagingChangesRange: Story = {
  render: () => <Card sx={{ p: 2 }}><Footer /></Card>,
  play: async ({ canvasElement, step }) => {
    const canvas = within(canvasElement);
    const next = canvas.getByRole("button", { name: /next page/i });
    const prev = canvas.getByRole("button", { name: /previous page/i });
    const range = () => canvasElement.querySelector(".kit-tfoot-range") as HTMLElement;

    await step("first page", async () => {
      await expect(range()).toHaveTextContent("1–10 of 137");
      await expect(prev).toBeDisabled();
      await expect(next).toBeEnabled();
    });

    await step("next advances the range", async () => {
      await userEvent.click(next);
      await waitFor(() => expect(range()).toHaveTextContent("11–20 of 137"));
      await expect(prev).toBeEnabled();
      await userEvent.click(next);
      await waitFor(() => expect(range()).toHaveTextContent("21–30 of 137"));
    });

    await step("prev walks it back", async () => {
      await userEvent.click(prev);
      await waitFor(() => expect(range()).toHaveTextContent("11–20 of 137"));
    });

    await step("last page is a partial range and next is disabled", async () => {
      for (let i = 0; i < 13; i += 1) await userEvent.click(next);
      await waitFor(() => expect(range()).toHaveTextContent("131–137 of 137"));
      await expect(next).toBeDisabled();
    });

    await step("changing rows per page resets to page 1", async () => {
      // The rows-per-page control is an MUI select (a combobox + listbox), not a
      // native <select>: open it and pick the option.
      await userEvent.click(await canvas.findByRole("combobox", { name: /rows per page/i }));
      await userEvent.click(await waitFor(() => canvas.getByRole("option", { name: "25" })));
      await waitFor(() => expect(range()).toHaveTextContent("1–25 of 137"));
      await expect(prev).toBeDisabled();
    });
  },
};

/** An empty table reads "0 of 0", never "1–0 of 0". */
export const EmptyRangeText: Story = {
  render: () => <Card sx={{ p: 2 }}><Footer total={0} /></Card>,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvasElement.querySelector(".kit-tfoot-range")).toHaveTextContent("0 of 0");
    await expect(canvas.getByRole("button", { name: /previous page/i })).toBeDisabled();
    await expect(canvas.getByRole("button", { name: /next page/i })).toBeDisabled();
  },
};

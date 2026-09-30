import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Typography from "@mui/material/Typography";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { TableFooter } from "@/components/app/table-footer";
import { DenseToggle } from "@/components/app/dense-toggle";
import { Frame, MOBILE, StateBlock, States } from "./_fixtures";

const meta = {
  title: "Kit/Tables/TableFooter",
  component: TableFooter,
  parameters: { docs: { description: { component: "Rows-per-page, visible range and prev/next for every Goat OS table." } } },
  decorators: [(S) => <Frame width={880}>{S()}</Frame>],
} satisfies Meta<typeof TableFooter>;
export default meta;
type Story = StoryObj<typeof meta>;

/** Stateful host so the arrows and the rows-per-page select actually work in the story. */
function Live({ total = 248, start = 1, rows = 25, left }: { total?: number; start?: number; rows?: number; left?: React.ReactNode }) {
  const [page, setPage] = React.useState(start);
  const [rpp, setRpp] = React.useState(rows);
  return (
    <TableFooter
      page={page}
      rowsPerPage={rpp}
      total={total}
      left={left}
      onPageChange={(p) => setPage(p)}
      onRowsPerPageChange={(n) => { setRpp(n); setPage(1); }}
    />
  );
}

export const Default: Story = { args: { page: 1, rowsPerPage: 25, total: 248, onPageChange: () => {} }, render: () => <Live /> };

export const AllStates: Story = {
  args: { page: 1, rowsPerPage: 25, total: 248, onPageChange: () => {} },
  render: () => (
    <States>
      <StateBlock label="First page — prev disabled">
        <TableFooter page={1} rowsPerPage={25} total={248} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </StateBlock>
      <StateBlock label="Middle page — both arrows enabled">
        <TableFooter page={4} rowsPerPage={25} total={248} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </StateBlock>
      <StateBlock label="Last page — next disabled (partial range)">
        <TableFooter page={10} rowsPerPage={25} total={248} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </StateBlock>
      <StateBlock label="Empty — 0 of 0, both arrows disabled">
        <TableFooter page={1} rowsPerPage={25} total={0} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </StateBlock>
      <StateBlock label="Single page — no rows-per-page control">
        <TableFooter page={1} rowsPerPage={25} total={9} onPageChange={() => {}} />
      </StateBlock>
      <StateBlock label="Many rows — 18,420 weighings">
        <TableFooter page={37} rowsPerPage={100} total={18420} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </StateBlock>
      <StateBlock label="Left slot — selection count + dense toggle">
        <Live total={248} left={<Typography variant="body2" sx={{ color: "text.secondary" }}>6 pens selected</Typography>} />
      </StateBlock>
    </States>
  ),
};

export const WithDenseToggle: Story = {
  args: { page: 2, rowsPerPage: 25, total: 248, onPageChange: () => {} },
  render: function Render() {
    const [dense, setDense] = React.useState(false);
    return <Live total={248} start={2} left={<DenseToggle checked={dense} onChange={setDense} />} />;
  },
};

/** Behaviour: the page arrows move the range text, and the ends disable. */
export const PagingBehaviour: Story = {
  args: { page: 1, rowsPerPage: 25, total: 248, onPageChange: () => {} },
  render: () => <Live total={248} />,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const prev = c.getByLabelText("Previous page");
    const next = c.getByLabelText("Next page");
    await expect(c.getByText("1–25 of 248")).toBeInTheDocument();
    await expect(prev).toBeDisabled();
    await userEvent.click(next);
    await expect(c.getByText("26–50 of 248")).toBeInTheDocument();
    await expect(prev).toBeEnabled();
    await userEvent.click(next);
    await expect(c.getByText("51–75 of 248")).toBeInTheDocument();
    await userEvent.click(prev);
    await expect(c.getByText("26–50 of 248")).toBeInTheDocument();
    // rows-per-page resets to page 1 and rewrites the range
    // MUI TextField select, not a native <select>.
    await userEvent.click(await c.findByRole("combobox", { name: "Rows per page" }));
    await userEvent.click(await waitFor(() => c.getByRole("option", { name: "50" })));
    await expect(c.getByText("1–50 of 248")).toBeInTheDocument();
  },
};

export const Mobile: Story = {
  args: { page: 3, rowsPerPage: 25, total: 248, onPageChange: () => {} },
  globals: MOBILE,
  render: () => (
    <States>
      <StateBlock label="Mobile 390 — range + arrows must not wrap off-screen">
        <Live total={248} start={3} />
      </StateBlock>
      <StateBlock label="Mobile — empty">
        <TableFooter page={1} rowsPerPage={25} total={0} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </StateBlock>
    </States>
  ),
};

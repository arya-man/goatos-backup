import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import * as React from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { TableFooter } from "@/components/app/table-footer";
import { DenseToggle } from "@/components/app/dense-toggle";
import { Canvas, MOBILE, Stack } from "./_data";

const meta: Meta<typeof TableFooter> = { title: "Kit/Pagination/TableFooter", component: TableFooter };
export default meta;
type Story = StoryObj<typeof meta>;

function Paged({ total = 164, initialRows = 25 }: { total?: number; initialRows?: number }) {
  const [page, setPage] = React.useState(1);
  const [rows, setRows] = React.useState(initialRows);
  const [dense, setDense] = React.useState(false);
  return (
    <TableFooter
      page={page}
      rowsPerPage={rows}
      total={total}
      onPageChange={setPage}
      onRowsPerPageChange={(n) => { setRows(n); setPage(1); }}
      left={<DenseToggle checked={dense} onChange={setDense} />}
    />
  );
}

export const States: Story = {
  render: () => (
    <Canvas>
      <Stack title="first page (prev disabled)">
        <TableFooter page={1} rowsPerPage={25} total={164} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </Stack>
      <Stack title="middle page">
        <TableFooter page={3} rowsPerPage={25} total={164} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </Stack>
      <Stack title="last page (next disabled)">
        <TableFooter page={7} rowsPerPage={25} total={164} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </Stack>
      <Stack title="empty table — reads 0 of 0">
        <TableFooter page={1} rowsPerPage={25} total={0} onPageChange={() => {}} onRowsPerPageChange={() => {}} />
      </Stack>
      <Stack title="single page, no rows-per-page control">
        <TableFooter page={1} rowsPerPage={25} total={9} onPageChange={() => {}} />
      </Stack>
      <Stack title="large total + left slot">
        <TableFooter page={42} rowsPerPage={100} total={128_400} onPageChange={() => {}} onRowsPerPageChange={() => {}} left={<span>18 kids selected</span>} />
      </Stack>
    </Canvas>
  ),
};

/** Page arrows must move the range text; rows-per-page resets to page 1. */
export const Pagination: Story = {
  render: () => (<Canvas><Paged /></Canvas>),
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    await expect(c.getByText("1–25 of 164")).toBeInTheDocument();
    await expect(c.getByRole("button", { name: "Previous page" })).toBeDisabled();

    await userEvent.click(c.getByRole("button", { name: "Next page" }));
    await waitFor(async () => { await expect(c.getByText("26–50 of 164")).toBeInTheDocument(); });

    await userEvent.click(c.getByRole("button", { name: "Next page" }));
    await waitFor(async () => { await expect(c.getByText("51–75 of 164")).toBeInTheDocument(); });

    await userEvent.click(c.getByRole("button", { name: "Previous page" }));
    await waitFor(async () => { await expect(c.getByText("26–50 of 164")).toBeInTheDocument(); });

    // MUI TextField select, not a native <select>.
    await userEvent.click(await c.findByRole("combobox", { name: "Rows per page" }));
    await userEvent.click(await waitFor(() => c.getByRole("option", { name: "50" })));
    await waitFor(async () => { await expect(c.getByText("1–50 of 164")).toBeInTheDocument(); });
  },
};

export const Mobile: Story = { ...States, ...MOBILE };

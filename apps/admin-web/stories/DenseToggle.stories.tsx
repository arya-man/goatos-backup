import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import * as React from "react";
import { expect, userEvent, within } from "storybook/test";
import { DenseToggle } from "@/components/app/dense-toggle";
import { Canvas, MOBILE, PEN_ROWS, Stack } from "./_data";

const meta: Meta<typeof DenseToggle> = { title: "Kit/DenseToggle", component: DenseToggle };
export default meta;
type Story = StoryObj<typeof meta>;

function Demo({ label }: { label?: string }) {
  const [dense, setDense] = React.useState(false);
  return (
    <Stack title={dense ? "dense: on" : "dense: off"}>
      <DenseToggle checked={dense} onChange={setDense} label={label} />
      <div data-dense={dense ? "" : undefined}>
        <Table>
          <TableHead><TableRow><TableCell>Pen</TableCell><TableCell>Kids</TableCell><TableCell>ADG</TableCell><TableCell>Vendor</TableCell></TableRow></TableHead>
          <TableBody>
            {PEN_ROWS.map((r) => (
              <TableRow key={r.pen}><TableCell>{r.pen}</TableCell><TableCell>{r.kids}</TableCell><TableCell>{r.adg} g/day</TableCell><TableCell>{r.vendor}</TableCell></TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </Stack>
  );
}

export const States: Story = {
  render: () => (
    <Canvas>
      <Demo />
      <Stack title="checked / unchecked (static) and a long custom label">
        <DenseToggle checked={false} onChange={() => {}} />
        <DenseToggle checked onChange={() => {}} />
        <DenseToggle checked onChange={() => {}} label="Compact rows for the Lim Chu Kang weighing table" />
      </Stack>
    </Canvas>
  ),
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const sw = c.getAllByRole("switch")[0];
    await expect(sw).not.toBeChecked();
    await userEvent.click(sw);
    await expect(sw).toBeChecked();
    await expect(c.getByText("dense: on")).toBeInTheDocument();
  },
};

export const Mobile: Story = { ...States, ...MOBILE };

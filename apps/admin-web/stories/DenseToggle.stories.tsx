import type { Meta, StoryObj } from "@storybook/nextjs-vite";
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
      <div className={dense ? "tablewrap kit-dense" : "tablewrap"}>
        <table className="tbl" style={{ width: "100%" }}>
          <thead><tr><th>Pen</th><th>Kids</th><th>ADG</th><th>Vendor</th></tr></thead>
          <tbody>
            {PEN_ROWS.map((r) => (
              <tr key={r.pen}><td>{r.pen}</td><td>{r.kids}</td><td>{r.adg} g/day</td><td>{r.vendor}</td></tr>
            ))}
          </tbody>
        </table>
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

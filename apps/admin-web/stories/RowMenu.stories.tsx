import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import * as React from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { Eye, Pencil, Syringe, Trash2 } from "lucide-react";
import { RowMenu } from "@/components/app/row-menu";
import { Canvas, MOBILE, PEN_ROWS, Stack } from "./_data";

const ACTIONS = [
  { label: "View pen", icon: <Eye size={16} />, onSelect: () => {} },
  { label: "Edit weighing session", icon: <Pencil size={16} />, onSelect: () => {} },
  { label: "Record CDT booster", icon: <Syringe size={16} />, onSelect: () => {} },
  { label: "Delete load", icon: <Trash2 size={16} />, danger: true, onSelect: () => {} },
  { label: "Hidden (disabled)", onSelect: () => {}, disabled: true },
];

const meta: Meta<typeof RowMenu> = { title: "Kit/RowMenu", component: RowMenu, args: { actions: ACTIONS } };
export default meta;
type Story = StoryObj<typeof meta>;

export const Alignment: Story = {
  render: () => (
    <Canvas>
      <Stack title="right-aligned (default) / left-aligned">
        <div style={{ display: "flex", justifyContent: "space-between", maxWidth: 420, paddingBottom: 220 }}>
          <RowMenu actions={ACTIONS} align="left" ariaLabel="Pen A1 actions" />
          <RowMenu actions={ACTIONS} ariaLabel="Pen B1 actions" />
        </div>
      </Stack>
    </Canvas>
  ),
};

function RowMenuTable() {
  const [last, setLast] = React.useState("—");
  return (
    <Stack title={`last action: ${last}`}>
      <div className="tablewrap">
        <table className="tbl" style={{ width: "100%" }}>
          <thead><tr><th>Pen</th><th>Kids</th><th>ADG</th><th /></tr></thead>
          <tbody>
            {PEN_ROWS.map((r) => (
              <tr key={r.pen}>
                <td>{r.pen}</td><td>{r.kids}</td><td>{r.adg} g/day</td>
                <td style={{ textAlign: "right" }}>
                  <RowMenu
                    ariaLabel={`${r.pen} actions`}
                    actions={ACTIONS.map((a) => ({ ...a, onSelect: () => setLast(`${r.pen}: ${a.label}`) }))}
                  />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Stack>
  );
}

export const InTableRow: Story = {
  render: () => (<Canvas><RowMenuTable /></Canvas>),
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const trigger = await c.findByLabelText("Pen B1 actions");
    await expect(trigger).toHaveAttribute("aria-expanded", "false");
    await userEvent.click(trigger);
    await waitFor(() => expect(trigger).toHaveAttribute("aria-expanded", "true"));
    const menu = await waitFor(() => {
      // The surface is portaled to <body> (it must escape card overflow/transforms), so the
      // document, not the canvas, is where an open menu lives.
      const el = document.body.querySelector<HTMLElement>('[role="menu"]');
      if (!el) throw new Error("row menu did not open");
      return el;
    });
    // the disabled action is not rendered at all
    await expect(within(menu).queryByText("Hidden (disabled)")).toBeNull();
    await expect(within(menu).getByText("Record CDT booster")).toBeInTheDocument();

    await userEvent.keyboard("{Escape}");
    await waitFor(async () => {
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
    });

    await userEvent.click(trigger);
    // The menu lives in a body portal: find the item document-wide.
    await userEvent.click(await within(document.body).findByText("Record CDT booster"));
    await waitFor(async () => {
      await expect(c.getByText("last action: Pen B1: Record CDT booster")).toBeInTheDocument();
    });
  },
};

export const Mobile: Story = { ...InTableRow, ...MOBILE };

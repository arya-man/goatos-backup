import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { Iconify } from "@/components/minimal/iconify";
import { RowMenu, type RowMenuAction } from "@/components/app/row-menu";
import { Frame, Labelled, mobile } from "../_fixtures/frame";
import { penRows } from "../_fixtures/goatos";

const actions: RowMenuAction[] = [
  { label: "Edit pen", onSelect: () => {}, icon: <Iconify icon="solar:pen-bold" /> },
  { label: "Record weighing", onSelect: () => {}, icon: <Iconify icon="solar:dumbbell-large-minimalistic-bold" /> },
  { label: "Record vaccination", onSelect: () => {}, icon: <Iconify icon="solar:medical-kit-bold" /> },
  { label: "Move to load", onSelect: () => {}, icon: <Iconify icon="carbon:delivery" /> },
  { label: "Export history", onSelect: () => {}, icon: <Iconify icon="solar:download-bold" /> },
  { label: "Remove from herd", onSelect: () => {}, icon: <Iconify icon="solar:trash-bin-trash-bold" />, danger: true },
];

const meta: Meta<typeof RowMenu> = {
  title: "Kit/Menus/RowMenu",
  component: RowMenu,
  parameters: { layout: "fullscreen", dualTheme: { height: 520 } },
  decorators: [
    (Story) => (
      <Frame>
        <Story />
      </Frame>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof RowMenu>;

export const Default: Story = {
  render: () => (
    <div style={{ height: 360, display: "flex", justifyContent: "flex-end", paddingRight: 24 }}>
      <RowMenu actions={actions} />
    </div>
  ),
};

export const Alignments: Story = {
  render: () => (
    <div style={{ height: 400, display: "flex", justifyContent: "space-between", padding: "0 24px" }}>
      <Labelled label="align=left"><RowMenu actions={actions.slice(0, 4)} align="left" ariaLabel="Left actions" /></Labelled>
      <Labelled label="align=right (default)"><RowMenu actions={actions.slice(0, 4)} ariaLabel="Right actions" /></Labelled>
    </div>
  ),
};

/** Disabled actions are dropped entirely, and long labels widen the surface. */
export const EdgeCases: Story = {
  render: () => (
    <div style={{ height: 400, display: "flex", gap: 48, padding: "0 24px" }}>
      <Labelled label="Single action"><RowMenu actions={[actions[0]]} ariaLabel="One action" /></Labelled>
      <Labelled label="Disabled entries hidden">
        <RowMenu ariaLabel="Filtered actions" actions={[actions[0], { ...actions[1], disabled: true }, { ...actions[5], disabled: true }, actions[4]]} />
      </Labelled>
      <Labelled label="Long labels">
        <RowMenu
          ariaLabel="Long actions"
          actions={[
            { label: "Export weighing history for Godel 1 - Part 2 (batch 2412)", onSelect: () => {}, icon: <Iconify icon="solar:download-bold" /> },
            { label: "Move every head in this pen to load 2419 — Hosur Farm Supply", onSelect: () => {}, icon: <Iconify icon="carbon:delivery" /> },
          ]}
        />
      </Labelled>
      <Labelled label="Custom trigger"><RowMenu actions={actions.slice(0, 3)} ariaLabel="Custom trigger">⋯</RowMenu></Labelled>
    </div>
  ),
};

/** Where it actually lives: the last cell of every table row. */
export const InTableRows: Story = {
  render: () => (
    <Card sx={{ p: 2 }}>
      <div style={{ overflowX: "auto" }}>
        <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13, minWidth: 560 }}>
          <thead>
            <tr style={{ textAlign: "left", color: "var(--fg-muted)" }}>
              {["Tag", "Pen", "Weight", "Status", ""].map((h, i) => (
                <th key={i} style={{ padding: "8px 6px", borderBottom: "1px solid var(--line)", whiteSpace: "nowrap" }}>{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {penRows.slice(0, 6).map((r) => (
              <tr key={r.tag}>
                <td style={{ padding: "6px", borderBottom: "1px solid var(--line2)" }}>{r.tag}</td>
                <td style={{ padding: "6px", borderBottom: "1px solid var(--line2)" }}>{r.shed}</td>
                <td style={{ padding: "6px", borderBottom: "1px solid var(--line2)" }}>{r.weightKg} kg</td>
                <td style={{ padding: "6px", borderBottom: "1px solid var(--line2)" }}>{r.status}</td>
                <td style={{ padding: "6px", borderBottom: "1px solid var(--line2)", textAlign: "right" }}>
                  <RowMenu actions={actions} ariaLabel={`Actions for ${r.tag}`} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Card>
  ),
  parameters: { dualTheme: { height: 620 } },
};

export const MobileDefault: Story = { ...Default, globals: mobile };
export const MobileInTableRows: Story = { ...InTableRows, globals: mobile, parameters: { dualTheme: { height: 620 } } };

/* ------------------------------------------------------------------ play -- */

/** Opens, lists only enabled actions, runs one, Escape closes, outside click closes. */
export const MenuBehaviour: Story = {
  render: () => {
    function Harness() {
      const [last, setLast] = React.useState("—");
      return (
        <div style={{ height: 380 }}>
          <div style={{ display: "flex", justifyContent: "flex-end", paddingRight: 24 }}>
            <RowMenu
              ariaLabel="Actions for SF-0048"
              actions={[
                { label: "Edit pen", onSelect: () => setLast("edit"), icon: <Iconify icon="solar:pen-bold" /> },
                { label: "Record weighing", onSelect: () => setLast("weigh"), icon: <Iconify icon="solar:dumbbell-large-minimalistic-bold" /> },
                { label: "Archive pen", onSelect: () => setLast("archive"), disabled: true },
                { label: "Remove from herd", onSelect: () => setLast("remove"), danger: true, icon: <Iconify icon="solar:trash-bin-trash-bold" /> },
              ]}
            />
          </div>
          <p data-testid="last" style={{ fontSize: 13, color: "var(--fg-muted)" }}>last: {last}</p>
        </div>
      );
    }
    return <Harness />;
  },
  play: async ({ canvasElement, step }) => {
    const canvas = within(canvasElement);
    // Menus are portaled to <body>; query the document for them.
    const page = within(document.body);
    const trigger = canvas.getByRole("button", { name: /actions for sf-0048/i });

    await step("opens", async () => {
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
      await userEvent.click(trigger);
      await waitFor(() => expect(trigger).toHaveAttribute("aria-expanded", "true"));
      const menu = page.getByRole("menu", { name: /actions for sf-0048/i });
      // The disabled action is not rendered at all.
      await expect(within(menu).getAllByRole("menuitem")).toHaveLength(3);
      await expect(within(menu).queryByRole("menuitem", { name: /archive pen/i })).toBeNull();
    });

    await step("Escape closes it", async () => {
      await userEvent.keyboard("{Escape}");
      await waitFor(() => expect(page.queryByRole("menu", { name: /actions for sf-0048/i })).toBeNull());
      await expect(trigger).toHaveAttribute("aria-expanded", "false");
    });

    await step("choosing an action runs it and closes the menu", async () => {
      await userEvent.click(trigger);
      await userEvent.click(await waitFor(() => page.getByRole("menuitem", { name: /record weighing/i })));
      await waitFor(() => expect(canvas.getByTestId("last")).toHaveTextContent("last: weigh"));
      await waitFor(() => expect(page.queryByRole("menu", { name: /actions for sf-0048/i })).toBeNull());
    });

    await step("outside pointer-down closes it", async () => {
      await userEvent.click(trigger);
      await waitFor(() => expect(page.getByRole("menu", { name: /actions for sf-0048/i })).toBeInTheDocument());
      await userEvent.click(canvas.getByTestId("last"));
      await waitFor(() => expect(page.queryByRole("menu", { name: /actions for sf-0048/i })).toBeNull());
    });
  },
};

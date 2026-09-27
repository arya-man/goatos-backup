import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import * as React from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { Baby, Scale, Syringe, Truck } from "lucide-react";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import { Canvas, MOBILE, PEN_ROWS, Stack } from "./_data";

const ITEMS = [
  { value: "weighing", label: "Weighing", icon: <Scale size={16} />, count: 148 },
  { value: "vaccination", label: "Vaccination", icon: <Syringe size={16} />, count: 12 },
  { value: "kids", label: "Kids", icon: <Baby size={16} />, count: 46 },
  { value: "procurement", label: "Procurement", icon: <Truck size={16} />, disabled: true },
];

const BODY: Record<string, React.ReactNode> = {
  weighing: <p>148 of 164 kids weighed today across Seletar Park. Average daily gain 176 g/day.</p>,
  vaccination: <p>12 CDT boosters overdue in Pen B2 and Pen C1.</p>,
  kids: (
    <ul style={{ margin: 0, paddingLeft: 18 }}>
      {PEN_ROWS.map((r) => <li key={r.pen}>{r.pen} — {r.kids} kids, ADG {r.adg} g/day</li>)}
    </ul>
  ),
  procurement: <p>3 loads awaiting vendor verification.</p>,
};

function Demo({ variant }: { variant: "underline" | "pill" }) {
  const [value, setValue] = React.useState("weighing");
  return (
    <Stack title={variant}>
      <TemplateTabs items={ITEMS} value={value} onChange={setValue} variant={variant} ariaLabel={`${variant} tabs`} />
      <TabPanel tabKey={value}>
        <div data-testid={`panel-${variant}`}>{BODY[value]}</div>
      </TabPanel>
    </Stack>
  );
}

const meta: Meta<typeof TemplateTabs> = { title: "Kit/AnimatedTabs", component: TemplateTabs };
export default meta;
type Story = StoryObj<typeof meta>;

export const Variants: Story = {
  render: () => (<Canvas><Demo variant="underline" /><Demo variant="pill" /></Canvas>),
};

export const ManyTabsOverflow: Story = {
  render: () => {
    const many = Array.from({ length: 12 }, (_, i) => ({ value: `shed-${i}`, label: `Pen ${String(i + 1).padStart(2, "0")}`, count: 100 + i * 4 }));
    return (
      <Canvas>
        <Stack title="12 tabs — the strip scrolls, the page must not">
          <TemplateTabs items={many} value="shed-0" ariaLabel="Pens" />
        </Stack>
        <Stack title="long labels">
          <TemplateTabs
            items={[
              { value: "a", label: "Kranji Livestock Supply Cooperative" },
              { value: "b", label: "Lim Chu Kang Goat Breeders Association" },
            ]}
            value="a"
            variant="pill"
            ariaLabel="Vendors"
          />
        </Stack>
      </Canvas>
    );
  },
};

/** Click each tab: aria-selected moves, the panel swaps, and the sliding indicator follows. */
export const SwitchesPanels: Story = {
  render: () => (<Canvas><Demo variant="underline" /></Canvas>),
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const tablist = c.getByRole("tablist", { name: "underline tabs" });
    const tab = (name: RegExp) => within(tablist).getByRole("tab", { name });

    await expect(tab(/Weighing/)).toHaveAttribute("aria-selected", "true");
    await expect(c.getByTestId("panel-underline")).toHaveTextContent("148 of 164 kids weighed");
    await userEvent.click(tab(/Vaccination/));
    await waitFor(async () => {
      await expect(tab(/Vaccination/)).toHaveAttribute("aria-selected", "true");
      await expect(c.getByTestId("panel-underline")).toHaveTextContent("12 CDT boosters overdue");
    });

    await userEvent.click(tab(/Kids/));
    await waitFor(async () => {
      await expect(tab(/Kids/)).toHaveAttribute("aria-selected", "true");
      await expect(c.getByTestId("panel-underline")).toHaveTextContent("Pen A1 — 46 kids");
    });

    await expect(tab(/Procurement/)).toBeDisabled();
  },
};

export const Mobile: Story = { ...Variants, ...MOBILE };

import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { Activity, Scale, Syringe, Truck, Users } from "lucide-react";
import { TrendChart } from "@/components/app/trend-chart";
import { TableSkeleton } from "@/components/app/skeletons";
import { AnimatedTabs, TabPanel, type AnimatedTabItem } from "@/components/minimal/list/animated-tabs";
import { Frame, Labelled, mobile } from "../_fixtures/frame";
import { adgWeeks, gPerDay, penRows, vendorLoads } from "../_fixtures/goatos";

const baseItems: AnimatedTabItem[] = [
  { value: "overview", label: "Overview" },
  { value: "weighing", label: "Weighing" },
  { value: "health", label: "Health" },
  { value: "procurement", label: "Procurement" },
];

const countItems: AnimatedTabItem[] = [
  { value: "due", label: "Due today", count: 128, icon: <Syringe size={16} /> },
  { value: "overdue", label: "Overdue", count: 14, icon: <Activity size={16} /> },
  { value: "done", label: "Done", count: "1,204", icon: <Scale size={16} /> },
  { value: "loads", label: "Loads in", count: 0, icon: <Truck size={16} /> },
];

function Tabs({ items = baseItems, variant = "underline", start = items[0]?.value }: { items?: AnimatedTabItem[]; variant?: "underline" | "pill"; start?: string }) {
  const [value, setValue] = React.useState(start ?? items[0].value);
  return <AnimatedTabs items={items} value={value} onChange={setValue} variant={variant} ariaLabel="Herd sections" />;
}

const meta: Meta<typeof AnimatedTabs> = {
  title: "Kit/Tabs/AnimatedTabs",
  component: AnimatedTabs,
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
type Story = StoryObj<typeof AnimatedTabs>;

export const Underline: Story = { render: () => <Tabs /> };
export const Pill: Story = { render: () => <Tabs variant="pill" /> };

/** Count chips: large, small, and a zero count that must still render. */
export const WithCounts: Story = {
  render: () => (
    <div style={{ display: "grid", gap: 28 }}>
      <Labelled label="Underline">
        <Tabs items={countItems} />
      </Labelled>
      <Labelled label="Pill">
        <Tabs items={countItems} variant="pill" />
      </Labelled>
    </div>
  ),
};

export const WithIconsOnly: Story = {
  render: () => (
    <Tabs
      items={[
        { value: "head", label: "Head", icon: <Users size={16} /> },
        { value: "weigh", label: "Weighing", icon: <Scale size={16} /> },
        { value: "vax", label: "Vaccination", icon: <Syringe size={16} /> },
      ]}
    />
  ),
};

/** Disabled tab: not clickable, and never takes the indicator. */
export const WithDisabledTab: Story = {
  render: () => (
    <Tabs
      items={[
        { value: "overview", label: "Overview" },
        { value: "weighing", label: "Weighing", count: 42 },
        { value: "finance", label: "Finance", disabled: true },
        { value: "health", label: "Health", count: 9 },
      ]}
    />
  ),
};

/** Active tab is the last one — the indicator has to start at the right edge. */
export const ActiveLast: Story = { render: () => <Tabs start="procurement" /> };

/** Long labels and many tabs: the strip scrolls horizontally instead of wrapping. */
export const ManyTabsOverflow: Story = {
  render: () => (
    <Tabs
      items={[
        { value: "overview", label: "Overview" },
        { value: "weighing", label: "Weighing & growth (ADG)", count: "1,284" },
        { value: "health", label: "Health and treatment register", count: 96 },
        { value: "vax", label: "Vaccination rounds", count: 128 },
        { value: "feed", label: "Milk feeding & creep", count: 54 },
        { value: "proc", label: "Procurement pipeline", count: 11 },
        { value: "sales", label: "Sales deals & tolerance", count: 7 },
        { value: "workforce", label: "Workforce coverage", count: 23 },
      ]}
    />
  ),
};

/** Two tabs only — the minimum. */
export const TwoTabs: Story = {
  render: () => <Tabs items={[{ value: "live", label: "Live", count: "2,302" }, { value: "archive", label: "Archived", count: 418 }]} />,
};

/* ------------------------------------------------------------ TabPanel ---- */

function Panels({ loading = false, empty = false }: { loading?: boolean; empty?: boolean }) {
  const [value, setValue] = React.useState("overview");
  return (
    <div style={{ display: "grid", gap: 16 }}>
      <AnimatedTabs items={countItemsForPanels} value={value} onChange={setValue} ariaLabel="Herd sections" />
      <TabPanel tabKey={value}>
        {loading ? (
          <TableSkeleton columns={4} rows={5} pager={false} />
        ) : empty ? (
          <Card sx={{ p: { xs: 2, sm: 3 } }}>
            <CardHeader sx={{ p: 0, mb: 2 }} title="Nothing here" subheader="No records for this section in the selected range" />
            <p style={{ margin: 0, color: "var(--fg-muted)" }}>Try a wider period or another park to see matching records.</p>
          </Card>
        ) : value === "overview" ? (
          <Card sx={{ p: { xs: 2, sm: 3 } }}>
            <CardHeader sx={{ p: 0, mb: 2 }} title="ADG by park" subheader="A tall panel, so the height animation is visible on switch" />
            <TrendChart data={adgWeeks} xKey="week" series={[{ key: "godel1", label: "Godel 1" }, { key: "godel2", label: "Godel 2" }]} valueFormat={gPerDay} height={280} />
          </Card>
        ) : value === "weighing" ? (
          <Card sx={{ p: { xs: 2, sm: 3 } }}>
            <CardHeader sx={{ p: 0, mb: 2 }} title="Weighing queue" subheader="Three rows — a much shorter panel" />
            <div style={{ display: "grid", gap: 8, fontSize: 14 }}>
              {penRows.slice(0, 3).map((r) => (
                <div key={r.tag} style={{ display: "flex", justifyContent: "space-between", borderBottom: "1px dashed var(--line)", paddingBottom: 6 }}>
                  <span>{r.tag} · {r.shed}</span>
                  <span>{r.weightKg} kg</span>
                </div>
              ))}
            </div>
          </Card>
        ) : value === "health" ? (
          <Card sx={{ p: { xs: 2, sm: 3 } }}>
            <CardHeader sx={{ p: 0, mb: 2 }} title="Treatment register" />
            <div style={{ display: "grid", gap: 8, fontSize: 14 }}>
              {penRows.slice(3, 12).map((r) => (
                <div key={r.tag} style={{ display: "flex", justifyContent: "space-between", borderBottom: "1px dashed var(--line)", paddingBottom: 6 }}>
                  <span>{r.tag} · {r.status}</span>
                  <span>{r.adg} g ADG</span>
                </div>
              ))}
            </div>
          </Card>
        ) : (
          <Card sx={{ p: { xs: 2, sm: 3 } }}>
            <CardHeader sx={{ p: 0, mb: 2 }} title="Vendors" />
            <div style={{ display: "grid", gap: 8, fontSize: 14 }}>
              {vendorLoads.map((v) => (
                <div key={v.vendor} style={{ display: "flex", justifyContent: "space-between", borderBottom: "1px dashed var(--line)", paddingBottom: 6 }}>
                  <span>{v.vendor}</span>
                  <span>{v.loads} loads · {v.head} head</span>
                </div>
              ))}
            </div>
          </Card>
        )}
      </TabPanel>
    </div>
  );
}

const countItemsForPanels: AnimatedTabItem[] = [
  { value: "overview", label: "Overview" },
  { value: "weighing", label: "Weighing", count: 3 },
  { value: "health", label: "Health", count: 9 },
  { value: "procurement", label: "Procurement", count: 5 },
];

export const WithContentTransition: Story = { render: () => <Panels />, parameters: { dualTheme: { height: 760 } } };
export const PanelLoading: Story = { render: () => <Panels loading /> };
export const PanelEmpty: Story = { render: () => <Panels empty /> };

/* ---------------------------------------------------------------- mobile -- */

export const MobileUnderline: Story = { render: () => <Tabs items={countItems} />, globals: mobile };
export const MobilePill: Story = { render: () => <Tabs items={countItems} variant="pill" />, globals: mobile };
export const MobileOverflowScroll: Story = { ...ManyTabsOverflow, globals: mobile };
export const MobileWithContentTransition: Story = { render: () => <Panels />, globals: mobile, parameters: { dualTheme: { height: 800 } } };

/* ------------------------------------------------------------------ play -- */

/** Click each tab: aria-selected moves, the indicator moves with it, the panel swaps. */
export const TabSwitching: Story = {
  render: () => <Panels />,
  parameters: { dualTheme: { height: 760 } },
  play: async ({ canvasElement, step }) => {
    const canvas = within(canvasElement);
    await waitFor(() => expect(canvas.getByRole("tab", { name: /overview/i })).toHaveAttribute("aria-selected", "true"));

    await step("Weighing", async () => {
      await userEvent.click(canvas.getByRole("tab", { name: /weighing/i }));
      await waitFor(() => expect(canvas.getByRole("tab", { name: /weighing/i })).toHaveAttribute("aria-selected", "true"));
      await expect(canvas.getByRole("tab", { name: /overview/i })).toHaveAttribute("aria-selected", "false");
      await waitFor(() => expect(canvas.getByText("Weighing queue")).toBeInTheDocument());
    });

    await step("Health", async () => {
      await userEvent.click(canvas.getByRole("tab", { name: /health/i }));
      await waitFor(() => expect(canvas.getByText("Treatment register")).toBeInTheDocument());
      // The outgoing panel lingers as an aria-hidden clone for the 180ms crossfade.
      await waitFor(() => expect(canvas.queryByText("Weighing queue")).toBeNull());
    });

    await step("Procurement", async () => {
      await userEvent.click(canvas.getByRole("tab", { name: /procurement/i }));
      await waitFor(() => expect(canvas.getByText("Ambika Livestock")).toBeInTheDocument());
    });

    await step("back to Overview", async () => {
      await userEvent.click(canvas.getByRole("tab", { name: /overview/i }));
      await waitFor(() => expect(canvas.getByText("ADG by park")).toBeInTheDocument());
    });
  },
};

/** A disabled tab must not activate on click. */
export const DisabledTabIsInert: Story = {
  ...WithDisabledTab,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const finance = canvas.getByRole("tab", { name: /finance/i });
    await expect(finance).toBeDisabled();
    await userEvent.click(finance, { pointerEventsCheck: 0 });
    await expect(finance).toHaveAttribute("aria-selected", "false");
    await expect(canvas.getByRole("tab", { name: /overview/i })).toHaveAttribute("aria-selected", "true");
  },
};

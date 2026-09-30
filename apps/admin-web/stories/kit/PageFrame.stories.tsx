import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { expect, within } from "storybook/test";
import Button from "@mui/material/Button";
import { KpiRowSkeleton, PageHeaderSkeleton, PageSkeleton, TableSkeleton } from "@/components/app/skeletons";
import { PageHeader } from "@/components/app/page-header";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget } from "@/components/app/kpi-widget";
import { TemplateTabs } from "@/components/app/template-tabs";
import { Iconify } from "@/components/minimal/iconify";
import { MOBILE } from "../_data";

/**
 * The page frame (FRAME-SPEC): a `.kit-page` content column with a 24px rhythm,
 * one PageHeader anatomy for every route — eyebrow · title · crumbs · actions, a tab strip
 * and a toolbar below, and deliberately no description slot.
 */
const meta: Meta<typeof PageHeader> = {
  title: "Kit/PageFrame",
  component: PageHeader,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <div className="main" style={{ background: "var(--bg)", color: "var(--fg)", minHeight: "100vh", padding: "clamp(16px, 2vw, 24px)" }}>
        <Story />
      </div>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof meta>;

const kpis = [
  { label: "Head on farm", value: 2302, unit: "head", trend: { value: 2.1, caption: "last 7 days" } },
  { label: "ADG (7d)", value: 147, unit: "g/day", trend: { value: -1.4, caption: "last 7 days" } },
  { label: "Due vaccinations", value: 128, unit: undefined, trend: { value: 4.8, caption: "this week" } },
];

function TabsDemo() {
  const [value, setValue] = React.useState("summary");
  return (
    <TemplateTabs
      ariaLabel="Herd analytics sections"
      value={value}
      onChange={setValue}
      items={[
        { value: "summary", label: "Summary" },
        { value: "breed", label: "Breed mix", count: 5 },
        { value: "sheds", label: "Pens", count: 12 },
        { value: "movements", label: "Movements", count: 48 },
      ]}
    />
  );
}

/** The standard page: header with crumbs + primary action, tab strip, KPI row, card, bar list. */
export const Default: Story = {
  render: () => (
    <div className="kit-page" aria-label="Herd analytics">
      <PageHeader
        title="Herd analytics"
        crumbs={[{ label: "Counts", href: "#" }, { label: "Herd analytics" }]}
        actions={<Button color="primary" variant="contained">Export</Button>}
        tabs={<TabsDemo />}
      />
      <KpiGrid>
        {kpis.map((k) => (
          <KpiWidget key={k.label} title={k.unit ? `${k.label} (${k.unit})` : k.label} total={k.value} />
        ))}
      </KpiGrid>
    </div>
  ),
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    await expect(c.getByRole("heading", { level: 1, name: "Herd analytics" })).toBeInTheDocument();
    await expect(c.getByRole("navigation", { name: "breadcrumb" })).toBeInTheDocument();
  },
};

/** Same frame at 390px: the tab strip scrolls (never wraps); a single action stays beside the title. */
export const Mobile: Story = { ...Default, ...MOBILE };

/** Header only, minimal: no eyebrow, no crumbs, no tabs — a settings-style page. */
export const HeaderOnly: Story = {
  render: () => (
    <div className="kit-page">
      <PageHeader title="Configuration" actions={<Button color="primary" variant="outlined">New table</Button>} />
      <Card sx={{ p: { xs: 2, sm: 3 } }}>
        <CardHeader sx={{ p: 0, mb: 2 }} title="Tables" />
        <div style={{ color: "var(--fg-muted)", fontSize: 14, minHeight: 120 }}>
          Configuration tables appear here after the schema registry loads.
        </div>
      </Card>
    </div>
  ),
};

/** L2/L3 page: the heading is the back link (template `CustomBreadcrumbs` `backHref`), no separate Back bar. */
export const DetailWithBack: Story = {
  render: () => (
    <div className="kit-page">
      <PageHeader
        title="CPT · Pen A2"
        backHref="#"
        crumbs={[{ label: "Vaccination", href: "#" }, { label: "CPT" }, { label: "Pen A2" }]}
        actions={<Button color="primary" variant="outlined">Export</Button>}
      />
    </div>
  ),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("heading", { level: 1 })).toHaveTextContent("CPT · Pen A2");
    await expect(canvasElement.querySelectorAll("a.minimal__breadcrumbs__back")).toHaveLength(1);
  },
};
export const DetailWithBackMobile: Story = { ...DetailWithBack, ...MOBILE };

/** Phone, one icon action: the button stays on the title row as a 44px icon-only square. */
export const MobileSingleIconAction: Story = {
  ...MOBILE,
  render: () => (
    <div className="kit-page">
      <PageHeader
        title="Herd analytics"
        crumbs={[{ label: "Counts", href: "#" }, { label: "Herd analytics" }]}
        actions={<Button color="primary" variant="contained" startIcon={<Iconify icon="solar:download-bold" width={16} />}>Export</Button>}
      />
    </div>
  ),
};

/** Loading: the SAME shell and header so the skeleton cannot bleed to the edges (FRAME-SPEC). */
export const Loading: Story = {
  render: () => (
    <PageSkeleton>
      <PageHeaderSkeleton />
      <KpiRowSkeleton count={4} />
      <TableSkeleton columns={6} rows={8} />
    </PageSkeleton>
  ),
};


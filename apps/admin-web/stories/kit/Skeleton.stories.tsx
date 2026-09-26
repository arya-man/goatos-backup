import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { expect, within } from "storybook/test";
import { Skeleton, SkeletonCard, SkeletonChart, SkeletonDrawer, SkeletonKpiRow, SkeletonList, SkeletonTable, SkeletonText } from "@/components/app/page-skeletons";
import { Frame, Grid, Labelled, mobile } from "../_fixtures/frame";

/**
 * Loading placeholders. Every shape is `aria-hidden`, so the play function
 * asserts the a11y tree stays empty — a screen reader must not narrate a loader.
 *
 * Column widths / row counts here mirror the real Goat OS tables so the loader
 * does not re-flow on hydration (pen table: tag, shed, vendor, weight, ADG,
 * status, row menu).
 */
const meta: Meta = {
  title: "Kit/Skeleton",
  parameters: { layout: "fullscreen", dualTheme: { height: 700 } },
  decorators: [
    (Story) => (
      <Frame>
        <Story />
      </Frame>
    ),
  ],
};
export default meta;
type Story = StoryObj;

/* --------------------------------------------------------------- atoms ---- */

export const Atom: Story = {
  render: () => (
    <Grid min={260}>
      <Labelled label="Default (16px bar)"><Skeleton width={220} /></Labelled>
      <Labelled label="Title bar"><Skeleton width="60%" height={24} radius={10} /></Labelled>
      <Labelled label="Avatar circle"><Skeleton width={44} height={44} radius="50%" /></Labelled>
      <Labelled label="Square thumb"><Skeleton width={72} height={72} radius={12} /></Labelled>
      <Labelled label="Full-width block"><Skeleton width="100%" height={120} radius={16} /></Labelled>
      <Labelled label="Hairline"><Skeleton width="100%" height={4} radius={2} /></Labelled>
    </Grid>
  ),
};

export const Text: Story = {
  render: () => (
    <Grid min={260}>
      <Labelled label="1 line"><SkeletonText lines={1} /></Labelled>
      <Labelled label="3 lines (default)"><SkeletonText /></Labelled>
      <Labelled label="8 lines"><SkeletonText lines={8} /></Labelled>
      <Labelled label="Narrow column"><SkeletonText lines={4} width={180} /></Labelled>
    </Grid>
  ),
};

/* ---------------------------------------------------------------- card ---- */

export const CardShape: Story = {
  render: () => (
    <Grid min={300}>
      <Labelled label="Default (3 lines)"><SkeletonCard /></Labelled>
      <Labelled label="1 line"><SkeletonCard lines={1} /></Labelled>
      <Labelled label="6 lines"><SkeletonCard lines={6} /></Labelled>
      <Labelled label="Fixed height"><SkeletonCard height={220} /></Labelled>
      <Labelled label="Custom children">
        <SkeletonCard>
          <Skeleton width="100%" height={96} radius={12} />
          <SkeletonText lines={2} />
        </SkeletonCard>
      </Labelled>
    </Grid>
  ),
};

/* ----------------------------------------------------------------- kpi ---- */

export const KpiRow: Story = {
  render: () => (
    <div style={{ display: "grid", gap: 28 }}>
      <Labelled label="4 tiles (default)"><SkeletonKpiRow /></Labelled>
      <Labelled label="4 tiles with spark block"><SkeletonKpiRow spark /></Labelled>
      <Labelled label="2 tiles"><SkeletonKpiRow count={2} /></Labelled>
      <Labelled label="6 tiles"><SkeletonKpiRow count={6} spark /></Labelled>
      <Labelled label="1 tile"><SkeletonKpiRow count={1} spark /></Labelled>
    </div>
  ),
};

/* --------------------------------------------------------------- chart ---- */

export const ChartShape: Story = {
  render: () => (
    <div style={{ display: "grid", gap: 28 }}>
      <Labelled label="Default (card, title, 12 bars)"><SkeletonChart /></Labelled>
      <Labelled label="With legend strip"><SkeletonChart withLegend /></Labelled>
      <Labelled label="No title"><SkeletonChart title={false} /></Labelled>
      <Labelled label="Tall, 24 bars"><SkeletonChart bars={24} height={280} withLegend /></Labelled>
      <Labelled label="Sparse, 4 bars"><SkeletonChart bars={4} height={140} /></Labelled>
      <Labelled label="card=false, inside a real Card">
        <Card sx={{ p: { xs: 2, sm: 3 } }}>
          <CardHeader sx={{ p: 0, mb: 2 }} title="ADG by park" subheader="Loading last 8 weighings" />
          <SkeletonChart card={false} withLegend />
        </Card>
      </Labelled>
    </div>
  ),
};

/* --------------------------------------------------------------- table ---- */

/** The real pen-table tracks, so hydration does not shift a single column. */
const PEN_TABLE_WIDTHS = ["120px", "2fr", "2fr", "96px", "88px", "120px", "44px"];

export const TableShape: Story = {
  render: () => (
    <div style={{ display: "grid", gap: 28 }}>
      <Labelled label="Pen table — real column widths (7 cols, 10 rows)">
        <Card sx={{ p: 2 }}>
          <SkeletonTable rows={10} widths={PEN_TABLE_WIDTHS} />
        </Card>
      </Labelled>
      <Labelled label="Default (2fr + 3 × 1fr)"><SkeletonTable /></Labelled>
      <Labelled label="No header"><SkeletonTable rows={4} header={false} /></Labelled>
      <Labelled label="cols=6, no widths"><SkeletonTable rows={5} cols={6} /></Labelled>
      <Labelled label="Two columns"><SkeletonTable rows={4} widths={["1fr", "120px"]} /></Labelled>
      <Labelled label="Single row (last page)"><SkeletonTable rows={1} widths={PEN_TABLE_WIDTHS} /></Labelled>
      <Labelled label="Long table (25 rows)"><SkeletonTable rows={25} widths={PEN_TABLE_WIDTHS} /></Labelled>
    </div>
  ),
};

/* ---------------------------------------------------------------- list ---- */

export const ListShape: Story = {
  render: () => (
    <Grid min={320}>
      <Labelled label="Default (5 rows, avatar)"><SkeletonList /></Labelled>
      <Labelled label="No avatar"><SkeletonList avatar={false} /></Labelled>
      <Labelled label="card=false"><SkeletonList card={false} /></Labelled>
      <Labelled label="1 row"><SkeletonList rows={1} /></Labelled>
      <Labelled label="12 rows"><SkeletonList rows={12} /></Labelled>
    </Grid>
  ),
};

/* -------------------------------------------------------------- drawer ---- */

export const DrawerShape: Story = {
  render: () => (
    <Grid min={380}>
      <Labelled label="Default (3 meta rows, 4 lines)">
        <div style={{ width: "min(480px, 100%)", background: "var(--paper)", borderRadius: "var(--r-lg)", boxShadow: "var(--shadow-card)" }}>
          <SkeletonDrawer />
        </div>
      </Labelled>
      <Labelled label="Dense (1 meta row, 2 lines)">
        <div style={{ width: "min(480px, 100%)", background: "var(--paper)", borderRadius: "var(--r-lg)", boxShadow: "var(--shadow-card)" }}>
          <SkeletonDrawer metaRows={1} lines={2} />
        </div>
      </Labelled>
      <Labelled label="Long (5 meta rows, 8 lines)">
        <div style={{ width: "min(480px, 100%)", background: "var(--paper)", borderRadius: "var(--r-lg)", boxShadow: "var(--shadow-card)" }}>
          <SkeletonDrawer metaRows={5} lines={8} />
        </div>
      </Labelled>
    </Grid>
  ),
};

/* ---------------------------------------------------------- whole page ---- */

/** What a `loading.tsx` for the weighing page actually renders. */
export const FullPageLoading: Story = {
  render: () => (
    <div style={{ display: "grid", gap: 20 }}>
      <SkeletonKpiRow count={4} spark />
      <div style={{ display: "grid", gap: 20, gridTemplateColumns: "repeat(auto-fit, minmax(min(420px, 100%), 1fr))" }}>
        <SkeletonChart withLegend />
        <SkeletonChart bars={6} withLegend />
      </div>
      <Card sx={{ p: 2 }}>
        <SkeletonTable rows={8} widths={PEN_TABLE_WIDTHS} />
      </Card>
    </div>
  ),
  parameters: { dualTheme: { height: 1000 } },
};

/* ---------------------------------------------------------------- mobile -- */

export const MobileKpiRow: Story = { ...KpiRow, globals: mobile, parameters: { dualTheme: { height: 980 } } };
export const MobileTable: Story = { ...TableShape, globals: mobile, parameters: { dualTheme: { height: 1000 } } };
export const MobileChart: Story = { ...ChartShape, globals: mobile, parameters: { dualTheme: { height: 1000 } } };
export const MobileList: Story = { ...ListShape, globals: mobile, parameters: { dualTheme: { height: 900 } } };
export const MobileDrawer: Story = { ...DrawerShape, globals: mobile, parameters: { dualTheme: { height: 900 } } };
export const MobileFullPage: Story = { ...FullPageLoading, globals: mobile, parameters: { dualTheme: { height: 1100 } } };

/* ------------------------------------------------------------------ play -- */

/** Loaders are decorative: nothing in the a11y tree, and the shimmer class is on. */
export const HiddenFromAssistiveTech: Story = {
  ...FullPageLoading,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.queryAllByRole("table")).toHaveLength(0);
    await expect(canvas.queryAllByRole("img")).toHaveLength(0);
    await expect(canvasElement.textContent?.trim()).toBe("");
    const bars = canvasElement.querySelectorAll(".kit-skeleton");
    await expect(bars.length).toBeGreaterThan(40);
    bars.forEach((b) => expect(b.closest("[aria-hidden='true']")).not.toBeNull());
  },
};

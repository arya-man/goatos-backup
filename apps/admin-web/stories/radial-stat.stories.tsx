import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { expect, waitFor } from "storybook/test";
import { RadialStat } from "@/components/app/radial-stat";
import { StatStripSkeleton } from "@/components/app/skeletons";
import { Frame, MOBILE, StateBlock, States } from "./_fixtures";

const meta = {
  title: "Kit/RadialStat",
  component: RadialStat,
  decorators: [(S) => <Frame width={760}>{S()}</Frame>],
} satisfies Meta<typeof RadialStat>;
export default meta;
type Story = StoryObj<typeof meta>;

const Row = ({ children }: { children: React.ReactNode }) => (
  <div style={{ display: "flex", flexWrap: "wrap", gap: 24, alignItems: "flex-end" }}>{children}</div>
);

export const Default: Story = {
  args: { value: 78, caption: "Weighing coverage — Kranji Park" },
};

/** /sales/buyer-analytics sends 68.7887…; the centre must read 69% like the aria-label. */
export const FractionalValue: Story = {
  args: { value: 68.7887408044551, tone: "success", caption: "Revenue from repeat buyers" },
  // The centre Apex draws must equal the gauge's aria-label (judge 4 P1-1): 69%, never 68.7887…%.
  play: async ({ canvasElement }) => {
    const centre = await waitFor(
      () => {
        const el = canvasElement.querySelector(".kit-radial .apexcharts-datalabel-value");
        if (!el?.textContent) throw new Error("gauge not drawn yet");
        return el;
      },
      { timeout: 8000 },
    );
    const gauge = canvasElement.querySelector(".kit-radial");
    await expect(centre.textContent).toBe("69%");
    await expect(gauge?.getAttribute("aria-label")).toBe("69%");
  },
};

export const AllStates: Story = {
  args: { value: 78 },
  render: () => (
    <States>
      <StateBlock label="Values — 0, 7, 50, 99.4, 100, out-of-range clamps">
        <Row>
          <RadialStat value={0} caption="Mandai Quarantine" />
          <RadialStat value={7} caption="Pen D-08" tone="error" />
          <RadialStat value={50} caption="Pen B-04" tone="warning" />
          <RadialStat value={99.4} digits={1} caption="Pen C-01" tone="info" />
          <RadialStat value={100} caption="Sungei Tengah" tone="success" />
          <RadialStat value={140} caption="Clamped" tone="violet" />
        </Row>
      </StateBlock>

      <StateBlock label="Fractional server value — centre prints the rounded figure, never the raw float">
        <Row>
          <RadialStat value={68.7887408044551} caption="Revenue from repeat buyers" tone="success" />
          <RadialStat value={68.7887408044551} digits={1} caption="One decimal" tone="info" />
        </Row>
      </StateBlock>

      <StateBlock label="Tones">
        <Row>
          {(["primary", "info", "success", "warning", "error", "violet", "neutral"] as const).map((tone, i) => (
            <RadialStat key={tone} tone={tone} value={40 + i * 8} size={96} caption={tone} />
          ))}
        </Row>
      </StateBlock>

      <StateBlock label="Sizes">
        <Row>
          <RadialStat value={64} size={72} caption="72" />
          <RadialStat value={64} size={132} caption="132" />
          <RadialStat value={64} size={200} caption="200" />
        </Row>
      </StateBlock>

      <StateBlock label="Custom centre label, series colour, long caption overflow">
        <Row>
          <RadialStat value={82} centerLabel="182 g" caption="ADG vs 220 g/day target" />
          <RadialStat value={57} tone="violet" caption="Vaccination — CDT booster" />
          <RadialStat value={41} caption="Tanjung Karang Goat Supply Cooperative (Selangor) intake progress for FY2026 Q3" />
        </Row>
      </StateBlock>

      <StateBlock label="Loading skeleton / no data">
        <Row>
          <StatStripSkeleton count={1} card={false} />
          <RadialStat value={0} tone="neutral" centerLabel="No data" caption="Pen E-12 — not weighed today" />
        </Row>
      </StateBlock>
    </States>
  ),
};

export const InCard: Story = {
  args: { value: 78 },
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Vaccination coverage" subheader="All parks · 18/09/2026" />
      <div style={{ display: "flex", gap: 32, flexWrap: "wrap", justifyContent: "center", marginTop: 12 }}>
        <RadialStat value={94} tone="success" caption="Kranji Park" />
        <RadialStat value={71} tone="primary" caption="Lim Chu Kang Park" />
        <RadialStat value={38} tone="warning" caption="Mandai Quarantine" />
      </div>
    </Card>
  ),
};

export const Mobile: Story = {
  args: { value: 78 },
  globals: MOBILE,
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Vaccination coverage" subheader="All parks" />
      <div style={{ display: "flex", gap: 20, flexWrap: "wrap", justifyContent: "center", marginTop: 12 }}>
        <RadialStat value={94} size={112} tone="success" caption="Kranji Park" />
        <RadialStat value={38} size={112} tone="warning" caption="Mandai Quarantine" />
      </div>
    </Card>
  ),
};

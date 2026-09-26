import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { ProgressRow } from "@/components/app/progress-row";
import { useTheme } from "@mui/material/styles";
import { chartColors } from "@/components/app/chart-colors";
import { ListRowsSkeleton } from "@/components/app/skeletons";
import { Frame, MOBILE, StateBlock, States } from "./_fixtures";

const meta = {
  title: "Kit/ProgressRow",
  component: ProgressRow,
  decorators: [(S) => <Frame width={560}>{S()}</Frame>],
} satisfies Meta<typeof ProgressRow>;
export default meta;

/** Chart series ramp from the theme palette, as legend swatches. */
function SeriesSwatches() {
  const colors = chartColors(useTheme()).slice(0, 5);
  return (
    <>
      {colors.map((c, i) => (
        <ProgressRow key={i} dot color={c} label={["Weighing", "Vaccination", "Procurement", "Health checks", "Transfers"][i]} value={[420, 318, 260, 190, 96][i]} total={420} />
      ))}
    </>
  );
}
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { label: "Kranji Park", value: 148, total: 210, hint: "148 of 210 head weighed" },
};

export const AllStates: Story = {
  args: { label: "Kranji Park", percent: 70 },
  render: () => (
    <States>
      <StateBlock label="Tones">
        {(["primary", "info", "success", "warning", "error", "violet", "neutral"] as const).map((tone, i) => (
          <ProgressRow key={tone} tone={tone} dot label={`${tone} — Pen ${String.fromCharCode(65 + i)}-0${i + 1}`} value={40 + i * 9} total={100} />
        ))}
      </StateBlock>

      <StateBlock label="Edge values — 0%, 1%, 50%, 99%, 100%, over-range clamps to 100%">
        <ProgressRow label="Mandai Quarantine" percent={0} value="0 of 54" />
        <ProgressRow label="Pen D-08" percent={1} value="1 of 61" />
        <ProgressRow label="Pen B-04" percent={50} />
        <ProgressRow label="Pen B-05" percent={99} />
        <ProgressRow label="Pen C-01" percent={100} tone="success" value="210 of 210" />
        <ProgressRow label="Over-target intake" percent={140} tone="warning" value="140%" />
      </StateBlock>

      <StateBlock label="Series colours (legend swatch) — chart parity">
        <SeriesSwatches />
      </StateBlock>

      <StateBlock label="Long label overflow + hint">
        <ProgressRow
          label="Tanjung Karang Goat Supply Cooperative (Selangor) — contracted intake for FY2026 quarter three"
          value={862}
          total={1240}
          hint="862 of 1,240 head received across Kranji, Lim Chu Kang, Sungei Tengah and Mandai Quarantine"
          tone="info"
          dot
        />
      </StateBlock>

      <StateBlock label="Loading skeleton">
        <ListRowsSkeleton rows={3} avatar={false} trailing={false} />
      </StateBlock>

      <StateBlock label="Empty / no data">
        <ProgressRow label="Pen E-12" percent={0} value="No weighing today" tone="neutral" hint="Last session 4 days ago" />
      </StateBlock>
    </States>
  ),
};

export const BreakdownCard: Story = {
  args: { label: "Kranji Park", percent: 70 },
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Weighing coverage by park" subheader="Today · 18/09/2026" />
      <div style={{ display: "grid", gap: 14, marginTop: 12 }}>
        <ProgressRow dot tone="primary" label="Kranji Park" value={410} total={520} />
        <ProgressRow dot tone="info" label="Lim Chu Kang Park" value={188} total={200} />
        <ProgressRow dot tone="violet" label="Sungei Tengah Park" value={397} total={397} />
        <ProgressRow dot tone="warning" label="Mandai Quarantine" value={12} total={115} hint="Vet hold in effect" />
      </div>
    </Card>
  ),
};

export const Mobile: Story = {
  args: { label: "Kranji Park", percent: 70 },
  globals: MOBILE,
  render: () => (
    <Card sx={{ p: { xs: 2, sm: 3 } }}>
      <CardHeader sx={{ p: 0, mb: 2 }} title="Weighing coverage" subheader="Today" />
      <div style={{ display: "grid", gap: 14, marginTop: 12 }}>
        <ProgressRow dot tone="primary" label="Kranji Park" value={410} total={520} />
        <ProgressRow dot tone="warning" label="Mandai Quarantine" value={12} total={115} hint="Vet hold in effect" />
        <ProgressRow dot tone="info" label="Tanjung Karang Goat Supply Cooperative (Selangor)" value={862} total={1240} />
      </div>
    </Card>
  ),
};

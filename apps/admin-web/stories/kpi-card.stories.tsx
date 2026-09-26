import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { Baby, HeartPulse, Scale, Syringe, Truck, Users } from "lucide-react";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { SkeletonKpiRow } from "@/components/app/page-skeletons";
import { ADG_WEEKS, Frame, LOADS_PER_DAY, MOBILE, StateBlock, States } from "./_fixtures";

const meta = {
  title: "Kit/KpiCard",
  component: KpiCard,
  decorators: [(S) => <Frame width={1080}>{S()}</Frame>],
} satisfies Meta<typeof KpiCard>;
export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    label: "Head weighed today",
    value: 1248,
    icon: <Scale size={18} />,
    tone: "primary",
    trend: { value: 6.2, caption: "vs yesterday" },
    sparkline: LOADS_PER_DAY,
    hint: "Across 4 parks",
  },
};

export const Variants: Story = {
  args: { label: "Head weighed today", value: 1248 },
  render: () => (
    <States>
      <StateBlock label="plain (default)">
        <KpiGrid>
          <KpiCard label="Head weighed today" value={1248} icon={<Scale size={18} />} tone="primary" trend={{ value: 6.2, caption: "vs yesterday" }} />
          <KpiCard label="Kids born (30d)" value={412} icon={<Baby size={18} />} tone="violet" trend={{ value: -2.1, caption: "vs prior 30d" }} />
          <KpiCard label="Loads inbound" value={9} icon={<Truck size={18} />} tone="info" hint="2 awaiting verification" />
          <KpiCard label="Mortality (30d)" value={1.4} digits={1} unit="%" icon={<HeartPulse size={18} />} tone="error" trend={{ value: -0.3, invert: true, caption: "vs prior 30d" }} />
        </KpiGrid>
      </StateBlock>

      <StateBlock label="tint — every tone, with watermark">
        <KpiGrid>
          {(["primary", "info", "success", "warning", "error", "violet", "neutral"] as const).map((tone) => (
            <KpiCard key={tone} variant="tint" tone={tone} watermark={<Scale size={96} />} label={`${tone} tone`} value={128} icon={<Scale size={18} />} hint="Pen C-01" />
          ))}
        </KpiGrid>
      </StateBlock>

      <StateBlock label="gradient — hero row">
        <KpiGrid>
          <KpiCard variant="gradient" tone="primary" watermark={<Scale size={110} />} label="ADG (herd)" value={182} unit="g/day" icon={<Scale size={18} />} trend={{ value: 4.8, caption: "vs last week" }} sparkline={ADG_WEEKS} sparkVariant="line" />
          <KpiCard variant="gradient" tone="success" watermark={<Syringe size={110} />} label="Vaccination coverage" value={94} unit="%" icon={<Syringe size={18} />} trend={{ value: 1.9 }} sparkline={LOADS_PER_DAY} />
          <KpiCard variant="gradient" tone="warning" watermark={<Users size={110} />} label="Vet workload" value={37} icon={<Users size={18} />} hint="Open health work items" />
        </KpiGrid>
      </StateBlock>

      <StateBlock label="Sparkline shapes — bar (default) and line, with and without icon">
        <KpiGrid>
          <KpiCard label="Loads per day" value={9} icon={<Truck size={18} />} sparkline={LOADS_PER_DAY} sparkVariant="bar" tone="info" />
          <KpiCard label="ADG trend" value={182} unit="g/day" icon={<Scale size={18} />} sparkline={ADG_WEEKS} sparkVariant="line" tone="primary" />
          <KpiCard label="No icon — spark right" value={318} sparkline={LOADS_PER_DAY} tone="violet" />
          <KpiCard label="No icon — line" value={176} sparkline={ADG_WEEKS} sparkVariant="line" tone="success" />
        </KpiGrid>
      </StateBlock>

      <StateBlock label="Interactive (hover/focus/press) — onClick and href">
        <KpiGrid>
          <KpiCard label="Open weighing queue" value={41} icon={<Scale size={18} />} onClick={() => {}} hint="Click or focus me" />
          <KpiCard label="Open procurement" value={6} icon={<Truck size={18} />} href="#procurement" tone="info" hint="Rendered as a link" />
        </KpiGrid>
      </StateBlock>

      <StateBlock label="Edge content — string value, zero, long label, custom format, footer">
        <KpiGrid>
          <KpiCard label="Status" value="Vet hold" tone="warning" icon={<HeartPulse size={18} />} />
          <KpiCard label="Head in Mandai Quarantine right now, including kids under 60 days" value={0} tone="neutral" hint="Nothing to show" />
          <KpiCard label="Revenue booked" value={182450} format={(n) => `S$${Math.round(n).toLocaleString()}`} tone="success" icon={<Truck size={18} />} />
          <KpiCard label="Vendors active" value={24} icon={<Users size={18} />} footer={<div className="small" style={{ marginTop: 10 }}>Top: Tanjung Karang Goat Supply Cooperative (Selangor)</div>} />
        </KpiGrid>
      </StateBlock>

      <StateBlock label="Loading skeleton">
        <SkeletonKpiRow count={4} spark />
      </StateBlock>

      <StateBlock label="Empty / error">
        <KpiGrid>
          <KpiCard label="Head weighed today" value="—" tone="neutral" hint="No weighing session started" />
          <KpiCard label="Mortality (30d)" value="Unavailable" tone="error" icon={<HeartPulse size={18} />} hint="Health service timed out" />
        </KpiGrid>
      </StateBlock>
    </States>
  ),
};

export const GridCounts: Story = {
  args: { label: "Head", value: 1 },
  render: () => (
    <States>
      {[1, 2, 3, 5, 6].map((n) => (
        <StateBlock key={n} label={`${n} card${n > 1 ? "s" : ""}`}>
          <KpiGrid>
            {Array.from({ length: n }, (_, i) => (
              <KpiCard key={i} label={["Head weighed", "Kids born", "Loads inbound", "Vet visits", "ADG", "Vendors"][i]} value={[1248, 412, 9, 37, 182, 24][i]} icon={<Scale size={18} />} tone="primary" />
            ))}
          </KpiGrid>
        </StateBlock>
      ))}
    </States>
  ),
};

export const Mobile: Story = {
  args: { label: "Head weighed today", value: 1248 },
  globals: MOBILE,
  render: () => (
    <KpiGrid>
      <KpiCard variant="gradient" tone="primary" watermark={<Scale size={110} />} label="Head weighed today" value={1248} icon={<Scale size={18} />} trend={{ value: 6.2, caption: "vs yesterday" }} sparkline={LOADS_PER_DAY} />
      <KpiCard label="Kids born (30d)" value={412} icon={<Baby size={18} />} tone="violet" trend={{ value: -2.1 }} />
      <KpiCard label="Head in Mandai Quarantine right now, including kids under 60 days" value={54} tone="warning" icon={<HeartPulse size={18} />} hint="Vet hold in effect" />
    </KpiGrid>
  ),
};

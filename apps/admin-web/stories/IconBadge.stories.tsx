import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { Iconify } from "@/components/minimal/iconify";
import type { KitTone } from "@/lib/tone";
import Card from "@mui/material/Card";
import { IconBadge } from "@/components/app/icon-badge";
import { Canvas, MOBILE, Row, Stack } from "./_data";

const TONES: KitTone[] = ["primary", "info", "success", "warning", "error", "violet", "neutral"];
const ICONS = [<Iconify icon="eva:smiling-face-fill" key="a" />, <Iconify icon="solar:dumbbell-large-minimalistic-bold" key="b" />, <Iconify icon="solar:medical-kit-bold" key="c" />, <Iconify icon="carbon:delivery" key="d" />, <Iconify icon="solar:tea-cup-bold" key="e" />, <Iconify icon="solar:home-2-outline" key="f" />, <Iconify icon="solar:users-group-rounded-bold" key="g" />];

const meta: Meta<typeof IconBadge> = { title: "Kit/IconBadge", component: IconBadge, args: { icon: <Iconify icon="solar:dumbbell-large-minimalistic-bold" /> } };
export default meta;
type Story = StoryObj<typeof meta>;

export const Tones: Story = {
  render: () => (
    <Canvas>
      {(["sm", "md", "lg"] as const).map((size) => (
        <Stack key={size} title={`size ${size}`}>
          <Row>
            {TONES.map((tone, i) => (
              <IconBadge key={tone} tone={tone} size={size} icon={ICONS[i]} />
            ))}
          </Row>
        </Stack>
      ))}
    </Canvas>
  ),
};

export const Shapes: Story = {
  render: () => (
    <Canvas>
      <Stack title="rounded vs circle">
        <Row>
          {TONES.map((tone, i) => (
            <IconBadge key={tone} tone={tone} shape="circle" size="lg" icon={ICONS[i]} />
          ))}
        </Row>
      </Stack>
    </Canvas>
  ),
};

/** In a KPI/list context, the badge must sit on the card surface without shifting the baseline. */
export const InContext: Story = {
  render: () => (
    <Canvas>
      {[
        { tone: "primary" as const, icon: <Iconify icon="eva:smiling-face-fill" />, title: "Kids weighed today", value: "148 of 164" },
        { tone: "warning" as const, icon: <Iconify icon="solar:medical-kit-bold" />, title: "CDT boosters overdue", value: "12 kids" },
        { tone: "info" as const, icon: <Iconify icon="carbon:delivery" />, title: "Loads awaiting verification", value: "3 vendors" },
      ].map((r) => (
        <Card key={r.title} sx={{ p: 3, display: "flex", gap: 1.75, alignItems: "center", maxWidth: 420 }}>
          <IconBadge tone={r.tone} icon={r.icon} size="lg" />
          <div style={{ display: "grid" }}>
            <strong style={{ fontSize: 18 }}>{r.value}</strong>
            <span style={{ opacity: 0.65, fontSize: 13 }}>{r.title}</span>
          </div>
        </Card>
      ))}
    </Canvas>
  ),
};

export const Mobile: Story = { ...Tones, ...MOBILE };

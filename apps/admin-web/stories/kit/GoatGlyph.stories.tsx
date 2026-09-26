import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import * as React from "react";

import { GoatGlyph } from "@/components/goat-glyph";
import { IconBadge } from "@/components/app/icon-badge";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { Frame, Labelled } from "../_fixtures/frame";

/** The Mesha goat silhouette at the three sizes the kit uses it: icon badge (24), stat ring (48), KPI watermark (96). */
const meta: Meta = { title: "Kit/GoatGlyph", parameters: { layout: "fullscreen" } };
export default meta;
type Story = StoryObj;

export const Sizes: Story = {
  render: () => (
    <Frame>
      <div style={{ display: "flex", gap: 40, alignItems: "flex-end", color: "var(--primary)" }}>
        <Labelled label="24"><GoatGlyph size={24} /></Labelled>
        <Labelled label="48"><GoatGlyph size={48} /></Labelled>
        <Labelled label="96"><GoatGlyph size={96} /></Labelled>
        <Labelled label="badge"><IconBadge icon={<GoatGlyph />} tone="primary" /></Labelled>
      </div>
      <div style={{ marginTop: 32 }}>
        <KpiGrid>
          <KpiCard variant="tint" tone="info" label="Animals sold" value={673} watermark={<GoatGlyph aria-hidden="true" />} hint="388 Sheep · 285 Goats" />
          <KpiCard tone="primary" icon={<GoatGlyph size={22} />} label="Live animals" value={1573} />
        </KpiGrid>
      </div>
    </Frame>
  ),
};

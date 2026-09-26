import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { LinkNavPending } from "@/components/app/link-nav-pending";
import { BodyPortal } from "@/components/app/body-portal";
import { ScrollEdges } from "@/components/app/scroll-edges";
import { Frame, mobile } from "../_fixtures/frame";

const meta: Meta = {
  title: "Kit/Navigation",
  parameters: { layout: "fullscreen", dualTheme: { height: 520 }, nextjs: { appDirectory: true } },
  decorators: [(Story) => (<Frame><Story /></Frame>)],
};
export default meta;
type Story = StoryObj;

/** Portalled overlay + scroll-edge fades + the nav-pending bar (idle). */
export const Overlays: Story = {
  render: () => (
    <div>
      <LinkNavPending />
      <ScrollEdges />
      <div className="tablewrap" style={{ overflowX: "auto" }}>
        <div style={{ width: 1400, padding: 16 }}>Wide content scrolls inside its own card; edges fade.</div>
      </div>
      <BodyPortal>
        <div role="status" style={{ position: "fixed", right: 16, bottom: 16, padding: "8px 16px", borderRadius: 8, background: "var(--paper)", boxShadow: "var(--shadow-pop)" }}>
          Portalled to body
        </div>
      </BodyPortal>
    </div>
  ),
};

export const OverlaysPhone: Story = { ...Overlays, globals: mobile };

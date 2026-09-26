import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { GenericPageSkeleton, ShellSkeleton } from "@/components/shell-skeleton";
import { mobile } from "../_fixtures/frame";

/** Loading state of the whole shell and of a generic page (loading.tsx fallbacks). */
const meta: Meta = {
  title: "Kit/ShellSkeleton",
  parameters: { layout: "fullscreen", dualTheme: { height: 800 } },
};
export default meta;
type Story = StoryObj;

export const Shell: Story = { render: () => <ShellSkeleton><GenericPageSkeleton /></ShellSkeleton> };
export const Page: Story = { render: () => <div style={{ background: "var(--bg)", minHeight: "100vh" }}><GenericPageSkeleton /></div> };
export const ShellPhone: Story = { ...Shell, globals: mobile };

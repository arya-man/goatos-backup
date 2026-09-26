import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import * as React from "react";
import { expect, userEvent, waitFor, within } from "storybook/test";
// The framework mocks next/navigation; `appDirectory: true` mounts the app-router provider,
// and getRouter() is how the play function inspects the navigation it caused.
import { getRouter } from "@storybook/nextjs-vite/navigation.mock";
import { LinkSelect } from "@/components/app/link-select";
import { Canvas, MOBILE, Row, Stack } from "./_data";

const PARK_LINKS = [
  { value: "seletar", label: "Seletar Park", href: "/weighing?park=seletar" },
  { value: "lim-chu-kang", label: "Lim Chu Kang Park", href: "/weighing?park=lim-chu-kang" },
  { value: "kranji", label: "Kranji Park", href: "/weighing?park=kranji" },
];

const LONG_LINKS = [
  { value: "batch-1", label: "Batch 2026-04 · Kranji Livestock Supply Cooperative", href: "/procurement/batch-1" },
  { value: "batch-2", label: "Batch 2026-05 · Lim Chu Kang Goat Breeders Association", href: "/procurement/batch-2" },
];

const meta: Meta<typeof LinkSelect> = {
  title: "Kit/LinkSelect",
  component: LinkSelect,
  parameters: { nextjs: { appDirectory: true, navigation: { pathname: "/weighing", query: { park: "seletar" } } } },
};
export default meta;
type Story = StoryObj<typeof meta>;

export const States: Story = {
  render: () => (
    <Canvas>
      <Stack title="park scope switcher (choosing an option router.push-es its href)">
        <Row>
          <LinkSelect label="Park" value="seletar" options={PARK_LINKS} />
          <LinkSelect label="Batch" value="batch-2" options={LONG_LINKS} minWidth={280} />
        </Row>
      </Stack>
      <Stack title="empty options">
        <LinkSelect label="Pen group" value="" options={[]} />
      </Stack>
    </Canvas>
  ),
};

export const Navigates: Story = {
  render: () => (
    <Canvas>
      <div style={{ paddingBottom: 200 }}>
        <LinkSelect label="Park" value="seletar" options={PARK_LINKS} />
      </div>
    </Canvas>
  ),
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const trigger = c.getByRole("combobox", { name: "Park" });
    await userEvent.click(trigger);
    await waitFor(async () => { await expect(c.getByRole("listbox", { name: "Park" })).toBeVisible(); });
    await userEvent.click(c.getByRole("option", { name: /Kranji Park/ }));
    await expect(trigger).toHaveAttribute("aria-expanded", "false");
    await waitFor(async () => {
      await expect(getRouter().push).toHaveBeenCalledWith("/weighing?park=kranji", { scroll: false });
    });
  },
};

export const Mobile: Story = { ...States, ...MOBILE };

import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, waitFor, within } from "storybook/test";
import { ThemeToggle } from "@/components/app/theme-toggle";
import { RetryButton } from "@/components/app/retry-button";
import { Canvas, Row, Stack } from "../_data";

/** Top-bar light/dark switch plus the read-only retry control used by error states. */
const meta: Meta<typeof ThemeToggle> = {
  title: "Kit/ThemeToggle",
  component: ThemeToggle,
  parameters: { layout: "fullscreen" },
  decorators: [
    (Story) => (
      <Canvas>
        <Story />
      </Canvas>
    ),
  ],
};
export default meta;
type Story = StoryObj<typeof meta>;

function Demo() {
  const [last, setLast] = React.useState<string>("none");
  return (
    <Stack title={`last change: ${last}`}>
      <Row>
        <ThemeToggle labelToLight="Switch to light theme" labelToDark="Switch to dark theme" onChange={(m) => setLast(m)} />
        <RetryButton label="Retry" />
      </Row>
    </Stack>
  );
}

export const Default: Story = {
  render: () => <Demo />,
  play: async ({ canvasElement }) => {
    const c = within(canvasElement);
    const before = document.documentElement.getAttribute("data-theme") ?? "dark";
    // The toggle reads the persisted theme after mount; wait until its label agrees with the
    // document (state), otherwise a click in the light lane "switches to light" and nothing moves.
    const expectedLabel = before === "light" ? "Switch to dark theme" : "Switch to light theme";
    const toggle = await c.findByRole("button", { name: expectedLabel });
    await userEvent.click(toggle);
    await waitFor(() => expect(document.documentElement.getAttribute("data-theme")).not.toBe(before));
    // Put it back so the capture stays on the requested theme.
    await userEvent.click(await c.findByRole("button", { name: before === "light" ? "Switch to light theme" : "Switch to dark theme" }));
    await waitFor(() => expect(document.documentElement.getAttribute("data-theme")).toBe(before));
  },
};

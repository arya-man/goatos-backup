import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, within } from "storybook/test";

import { ToxinProofTile } from "@/features/verification-review/toxin-proof-tile";
import { Frame, MOBILE } from "./_fixtures";

/**
 * The toxin review proof, shown IN PLACE under its step (main a222fbdd4), rebuilt in sx (guard
 * toxin-proof-tile-sx). A portrait phone photo (720x1280) must sit whole inside the fixed 4:3 stage,
 * letterboxed, never at its intrinsic height; a video is a play tile until pressed.
 */
const PORTRAIT =
  "data:image/svg+xml;utf8," +
  encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="720" height="1280"><rect width="720" height="1280" fill="#3a6"/><text x="360" y="640" font-size="80" text-anchor="middle" fill="#fff">strip</text></svg>');

const meta = {
  title: "Features/Verification/ToxinProofTile",
  component: ToxinProofTile,
  decorators: [(S: () => React.ReactElement) => <Frame>{S()}</Frame>],
  args: { kind: "photo", src: PORTRAIT, proofRef: "proof-1", label: "Read the strip", playLabel: "Play video" },
} satisfies Meta<typeof ToxinProofTile>;
export default meta;
type Story = StoryObj<typeof meta>;

async function photoFitsStage(canvasElement: HTMLElement) {
  const img = await within(canvasElement).findByAltText("Read the strip");
  const stage = img.closest("[data-proof-kind]") as HTMLElement;
  const s = stage.getBoundingClientRect();
  const i = img.getBoundingClientRect();
  // Pinned: the image box IS the stage (contain letterboxes inside it), never taller than it.
  await expect(Math.round(i.height)).toBe(stage.clientHeight);
  await expect(Math.round(s.width / s.height * 100) / 100).toBeCloseTo(1.33, 1);
}

export const PortraitPhoto: Story = { play: async ({ canvasElement }) => photoFitsStage(canvasElement) };
export const PortraitPhotoPhone: Story = { parameters: MOBILE, play: async ({ canvasElement }) => photoFitsStage(canvasElement) };
export const VideoBeforePlay: Story = {
  args: { kind: "video", src: "/missing.mp4" },
  play: async ({ canvasElement }) => {
    const button = within(canvasElement).getByRole("button", { name: "Play video" });
    await expect(button.getBoundingClientRect().height).toBeGreaterThanOrEqual(44);
    await expect(canvasElement.querySelector("video")).toBeNull();
  },
};

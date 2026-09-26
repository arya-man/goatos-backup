import * as React from "react";
import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, within } from "storybook/test";

import { ApprovalsQueueTable } from "@/features/approvals/approvals-queue-table";
import { approvalSubject } from "@/features/approvals/approval-display";
import type { AdminWebApprovalItem } from "@/lib/api/server";
import { Frame, MOBILE } from "./_fixtures";

/**
 * The /approvals queue from a FIXTURE (no staging writes): one birth, one death and one shifting
 * request, including a long pen-move subject. At phone width each request is a labelled card; the
 * 390px story pins that layout (no sideways scroll, 44px Review tap target, long subject wraps).
 */
const meta = {
  title: "Features/Approvals/Queue",
  parameters: { nextjs: { appDirectory: true } },
  decorators: [
    (S: () => React.ReactElement) => (
      // `.main` is the shell's content column; the phone card rules are scoped to it.
      <div className="main" style={{ overflow: "visible" }}>
        <Frame>{S()}</Frame>
      </div>
    ),
  ],
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

const PARK = "11111111-1111-4111-8111-111111111111";
const SHED_A = "22222222-2222-4222-8222-222222222222";
const SHED_B = "33333333-3333-4333-8333-333333333333";
const LOCATIONS: Record<string, string> = {
  [PARK]: "Channapatna",
  [SHED_A]: "Mandela 1 - Part 3",
  [SHED_B]: "Godel 2 - Part 1",
};

const ITEMS: AdminWebApprovalItem[] = [
  {
    approval_request_id: "ap-1",
    request_type: "shifting",
    status: "pending",
    raised_by_user_id: "u-1",
    raised_at: "2026-09-24T04:35:00Z",
    summary: { source_park_id: PARK, source_shed_id: SHED_A, destination_shed_id: SHED_B },
  } as AdminWebApprovalItem,
  {
    approval_request_id: "ap-2",
    request_type: "birth",
    status: "pending",
    raised_by_user_id: "u-2",
    raised_at: "2026-09-24T09:10:00Z",
    summary: { park_id: PARK },
  } as AdminWebApprovalItem,
  {
    approval_request_id: "ap-3",
    request_type: "death",
    status: "approved",
    raised_by_user_id: "u-3",
    raised_at: "2026-09-23T13:45:00Z",
    subject_animal_location: "Channapatna · Castro 2",
    summary: {},
  } as AdminWebApprovalItem,
];

const SUBJECTS = Object.fromEntries(ITEMS.map((item) => [item.approval_request_id, approvalSubject(item, LOCATIONS)]));

export const Queue: Story = {
  render: () => <ApprovalsQueueTable items={ITEMS} ok searchParams={{}} subjects={SUBJECTS} />,
};

export const Empty: Story = {
  render: () => <ApprovalsQueueTable items={[]} ok searchParams={{}} subjects={SUBJECTS} />,
};

export const Phone390: Story = {
  globals: MOBILE,
  render: () => <ApprovalsQueueTable items={ITEMS} ok searchParams={{}} subjects={SUBJECTS} />,
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    const reviews = await canvas.findAllByRole("link", { name: "Review" });
    await expect(reviews).toHaveLength(3);
    // The visual lane also renders this story at 1440; the card rules apply at phone width only.
    if (canvasElement.ownerDocument.defaultView!.innerWidth >= 600) return;
    for (const link of reviews) {
      const r = link.getBoundingClientRect();
      await expect(r.height).toBeGreaterThanOrEqual(44);
    }
    const doc = canvasElement.ownerDocument.documentElement;
    await expect(doc.scrollWidth - doc.clientWidth).toBeLessThanOrEqual(0);
  },
};

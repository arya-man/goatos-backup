import type { ReactNode } from "react";

import { Label, type LabelColor } from "@/components/minimal/label";

export function reviewStatusColor(status: string): LabelColor {
  if (status === "rejected") return "error";
  if (status === "approved") return "success";
  if (status === "cancelled") return "default";
  return "warning";
}

/** Review status pill (Approvals + Verify): the template soft Label in the status colour. */
export function StatusChip({ status, children }: { status: string; children?: ReactNode }) {
  return (
    <Label variant="soft" color={reviewStatusColor(status)} sx={{ textTransform: "capitalize" }}>
      {children ?? status}
    </Label>
  );
}

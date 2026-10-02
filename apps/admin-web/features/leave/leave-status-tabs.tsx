"use client";
// telemetry:exempt presentational wrapper; the leave page's own telemetry and route boundary own it.

import { useRef } from "react";
import Box from "@mui/material/Box";

import { TemplateTabs, type TemplateTabsProps } from "@/components/app/template-tabs";
import { useSelectedTabInView } from "@/features/leadership-tasks/selected-tab-in-view";

/**
 * The leave list's status strip, with its selected tab kept in view: on a phone the strip shows about
 * three statuses, and /leave?status=withdrawn opened with no tab visibly selected (PR #294 P4).
 */
export function LeaveStatusTabs(props: TemplateTabsProps) {
  const ref = useRef<HTMLDivElement>(null);
  useSelectedTabInView(ref, props.value);
  return (
    <Box ref={ref} sx={{ minWidth: 0 }}>
      <TemplateTabs {...props} />
    </Box>
  );
}

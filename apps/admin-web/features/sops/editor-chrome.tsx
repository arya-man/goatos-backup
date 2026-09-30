"use client";

import type { ReactNode } from "react";
import { PageHeader } from "@/components/app/page-header";
import { SegmentTabs } from "@/components/app/list/segment-tabs";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import { Label } from "@/components/minimal/label";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import Stack from "@mui/material/Stack";
import { StickyBar } from "./editor-parts";

/** `title` is the option's backend description (e.g. a toxin step kind's), shown as the menu item tooltip. */
type SelectOption = { value: string; label: string; title?: string };

/**
 * Shared chrome for every SOP editor surface (builder, inspection, weighing, feed, operator
 * steps): the one page header (template `CustomBreadcrumbs` via `PageHeader`, heading = back link to
 * the library) plus the editor's explainer in a template info `Alert`.
 *
 * Presentation only: nothing here touches SOP state, drag ordering or upload behaviour.
 */
export function EditorHeader({
  crumbs,
  title,
  subtitle,
  code,
  version,
  notice,
  backHref,
  actions,
}: {
  crumbs: string[];
  title: string;
  subtitle?: string;
  code?: string;
  version?: string;
  /** Pass-through notice (what publishing changes). */
  notice?: string;
  backHref: string;
  actions?: ReactNode;
}) {
  return (
    <>
      <div>
        <PageHeader
          title={title}
          backHref={backHref}
          crumbs={crumbs.map((label, index) => ({ label, href: index === 1 ? backHref : undefined }))}
          actions={
            code || version || actions ? (
              <>
                {code ? <Label variant="soft">{code}</Label> : null}
                {version ? <Label variant="soft" color="info">{version}</Label> : null}
                {actions}
              </>
            ) : undefined
          }
        />
      </div>
      {subtitle || notice ? (
        <Alert severity="info">
          {subtitle}
          {notice ? <Box component="span" sx={{ display: "block" }}>{notice}</Box> : null}
        </Alert>
      ) : null}
    </>
  );
}

/** LIST / FLOW switch for the studio editors: the kit pill strip, in the header's action slot. */
export function StudioViewToggle({
  label,
  listLabel,
  flowLabel,
  value,
  onChange,
}: {
  label: string;
  listLabel: string;
  flowLabel: string;
  value: "list" | "flow";
  onChange: (next: "list" | "flow") => void;
}) {
  return (
    <SegmentTabs
      ariaLabel={label}
      value={value}
      tabs={[
        { value: "list", label: listLabel, onClick: () => onChange("list"), testId: "studio-view-list" },
        { value: "flow", label: flowLabel, onClick: () => onChange("flow"), testId: "studio-view-flow" },
      ]}
    />
  );
}

/**
 * MUI TextField select for the dense question-card rows the editors are built from.
 */
export function InlineSelect({
  label,
  value,
  options,
  onChange,
  disabled,
  title,
  minWidth = 140,
}: {
  label: string;
  value: string;
  options: readonly SelectOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
  title?: string;
  minWidth?: number;
}) {
  return (
    <TextField
      select
      label={label}
      value={value}
      disabled={disabled}
      title={title}
      onChange={(event) => onChange(event.target.value)}
      sx={{ minWidth: { xs: 0, sm: minWidth }, flex: "0 0 auto", alignSelf: "flex-start", width: "fit-content", maxWidth: 1 }}
      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
    >
      {options.map((option) => (
        <MenuItem key={option.value} value={option.value} title={option.title}>
          {option.label}
        </MenuItem>
      ))}
    </TextField>
  );
}

/** MUI TextField select with its floating label, for card bodies and filter rows. */
export function FieldSelect({
  label,
  value,
  options,
  onChange,
  disabled,
  title,
  minWidth = 160,
}: {
  label: string;
  value: string;
  options: readonly SelectOption[];
  onChange: (value: string) => void;
  disabled?: boolean;
  title?: string;
  minWidth?: number;
}) {
  return (
    <TextField
      select
      label={label}
      value={value}
      disabled={disabled}
      title={title}
      onChange={(event) => onChange(event.target.value)}
      sx={{ mt: 1, minWidth: { xs: 0, sm: minWidth }, flex: "0 0 auto", alignSelf: "flex-start", width: "fit-content", maxWidth: 1 }}
      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
    >
      {options.map((option) => (
        <MenuItem key={option.value} value={option.value} title={option.title}>
          {option.label}
        </MenuItem>
      ))}
    </TextField>
  );
}

export function FieldRow({ children }: { children: ReactNode }) {
  return (
    <Stack direction="row" spacing={1.75} useFlexGap sx={{ flexWrap: "wrap", alignItems: "flex-end", mt: 1.25 }}>
      {children}
    </Stack>
  );
}

/** Save / dry-run / publish bar that stays reachable while a long editor is scrolled (template Card, editor-parts). */
export const StickyActions = StickyBar;

export { BodyPortal } from "@/components/app/body-portal";


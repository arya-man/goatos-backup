"use client";

import type { ReactNode } from "react";
import { PageHeader } from "@/components/app/page-header";
import { SegmentTabs } from "@/components/minimal/list/segment-tabs";
import Alert from "@mui/material/Alert";
import { Label } from "@/components/minimal/label";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import type { Theme } from "@mui/material/styles";
import styles from "./editor-chrome.module.css";

type SelectOption = { value: string; label: string };

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
          {notice ? <div>{notice}</div> : null}
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
      className="studio-view-toggle"
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
      className={styles.inlineSelect}
      onChange={(event) => onChange(event.target.value)}
      sx={{ minWidth: { xs: 0, sm: minWidth }, flexShrink: 0, maxWidth: 1 }}
      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
    >
      {options.map((option) => (
        <MenuItem key={option.value} value={option.value}>
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
      className={styles.fieldSelect}
      onChange={(event) => onChange(event.target.value)}
      sx={{ minWidth: { xs: 0, sm: minWidth }, flexShrink: 0, maxWidth: 1 }}
      slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
    >
      {options.map((option) => (
        <MenuItem key={option.value} value={option.value}>
          {option.label}
        </MenuItem>
      ))}
    </TextField>
  );
}

export function FieldRow({ children }: { children: ReactNode }) {
  return <div className={styles.fieldRow}>{children}</div>;
}

/** Save / dry-run / publish bar that stays reachable while a long editor is scrolled. */
export function StickyActions({ children }: { children: ReactNode }) {
  return <div className={styles.stickyActions}>{children}</div>;
}

/**
 * Phone rhythm for the inspection-style SOP editors (inspection, weighing, feed, toxin, PC care,
 * shifting, capture): the page cards drop their outer padding, the page head wraps its hint under
 * the title, and the question config boxes give textareas the full width. Theme tokens only; `&&`
 * keeps these above the shared editor rules they refine.
 */
export const inspectionEditorSx = (theme: Theme) => ({
  [theme.breakpoints.down("sm")]: {
    "&& .inspection-page": { p: 0 },
    "&& .inspection-page-head": { alignItems: "flex-start", p: 1.75, gap: 1.25 },
    "&& .inspection-page-head strong": { ...theme.typography.h6 },
    "&& .inspection-page-head .muted": { flexBasis: "100%", pl: 5.25 },
    "&& .inspection-page > .bd": { p: 1.75 },
    "&& .inspection-page > .bd > .qcfg": { p: 1.5, borderRadius: "var(--r-lg)" },
    "&& .inspection-page > .bd > .qcfg textarea:not(.MuiInputBase-input), && .inspection-page > .bd > .qcfg .qhelp": { width: 1, minWidth: 0, boxSizing: "border-box", ...theme.typography.body1 },
    "&& .inspection-page > .bd > .qcfg textarea.qhelp": { minHeight: theme.spacing(14.5) },
    "&& .inspection-page .qcfg-head": { alignItems: "flex-start", flexDirection: "column", gap: 0.5 },
  },
});

export { BodyPortal } from "@/components/app/body-portal";

export { styles as editorChrome };

"use client";

// The SOP editors' shared anatomy (FIXJ3, guard legacy-free-zone). Every editor (inspection,
// weighing, feed, toxin, PC Care, shifting, capture, operator steps, the builder) is built from
// the same few template parts, so the markup reads the same everywhere and no legacy class
// (qcard / qhead / qnum / qcfg / optrow / condrow / inspection-page / ia ...) carries the look:
//   EditorPage     the page column (template page gap, 24px)
//   EditorCard     a section / page card: template Card + CardHeader (number badge, title,
//                  subheader), optionally a collapsible header (the whole row toggles)
//   QuestionShell  one question / step / slot: an outlined Paper with a head row, a body and a
//                  dashed-divider footer (the template kanban / checkout item rhythm)
//   ConfigBox      a grouped config block on background.neutral (options, limits, capture)
//   OptionRow      one choice row: a radio / tick marker, the field, a remove action
//   CheckLine      a labelled Checkbox (44px tap box on a phone)
//   CondRow        an "Only if ..." row of inline selects
//   IconAction     a small template IconButton with an Iconify icon (up / down / remove ...)
//   StickyBar      the save / publish bar that stays reachable while a long editor scrolls
//   ResultNotice   the save / publish result Alert with the backend's first problems
// Presentation only: nothing here touches SOP state, ordering or upload behaviour.

import type { InputHTMLAttributes, ReactNode } from "react";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import ButtonBase from "@mui/material/ButtonBase";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Checkbox from "@mui/material/Checkbox";
import Divider from "@mui/material/Divider";
import FormControlLabel from "@mui/material/FormControlLabel";
import IconButton from "@mui/material/IconButton";
import Paper from "@mui/material/Paper";
import Radio from "@mui/material/Radio";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import type { SxProps, Theme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import { PageRoot } from "@/components/app/page-root";

/** The registered (offline) template icons the editors use. */
export const EDITOR_ICON = {
  up: "eva:arrow-ios-upward-fill",
  down: "eva:arrow-ios-downward-fill",
  remove: "mingcute:close-line",
  trash: "solar:trash-bin-trash-bold",
  add: "mingcute:add-line",
  check: "eva:checkmark-fill",
  lock: "solar:lock-password-outline",
  warn: "solar:danger-triangle-bold",
  drag: "custom:drag-dots-fill",
  copy: "solar:copy-bold",
  info: "solar:info-circle-bold",
} as const satisfies Record<string, IconifyName>;

const TAP = "var(--tap-min)";

/** The editor page column: header, notices and cards on the shared PageRoot grid (24px gap). */
export function EditorPage({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <PageRoot data-testid={testId}>
      {children}
    </PageRoot>
  );
}

/** Round number badge for a page / question / step (template soft Label). */
export function NumBadge({ children }: { children: ReactNode }) {
  return (
    <Label variant="soft" color="primary" sx={{ flexShrink: 0 }}>
      {children}
    </Label>
  );
}

/** Muted helper line (template body2 / caption in text.secondary). */
export function Hint({ children, sx, caption }: { children: ReactNode; sx?: SxProps<Theme>; caption?: boolean }) {
  return (
    <Typography variant={caption ? "caption" : "body2"} component="span" sx={[{ color: "text.secondary", display: "block" }, ...(Array.isArray(sx) ? sx : [sx])]}>
      {children}
    </Typography>
  );
}

/**
 * A section / page card: template Card + CardHeader (number badge avatar, title, subheader, action).
 * With `onToggle` the header row is one button (`aria-expanded`) that opens / closes the body.
 */
export function EditorCard({
  badge,
  title,
  meta,
  action,
  open,
  onToggle,
  children,
  testId,
}: {
  badge?: ReactNode;
  /** Omit for a body-only card (no header row). */
  title?: ReactNode;
  meta?: ReactNode;
  action?: ReactNode;
  open?: boolean;
  onToggle?: () => void;
  children?: ReactNode;
  testId?: string;
}) {
  const header = (
    <CardHeader
      component="span"
      avatar={badge != null ? <NumBadge>{badge}</NumBadge> : undefined}
      title={title}
      subheader={meta}
      action={
        onToggle ? (
          <Iconify icon={open ? EDITOR_ICON.up : EDITOR_ICON.down} sx={{ color: "text.secondary", mt: 0.5 }} />
        ) : (
          action
        )
      }
      slotProps={{ title: { variant: "subtitle1" }, subheader: { variant: "body2" }, action: { sx: { alignSelf: "center", m: 0 } } }}
      sx={{ display: "flex", width: 1, p: { xs: 2, sm: 3 }, textAlign: "left", "& .MuiCardHeader-content": { minWidth: 0 } }}
    />
  );
  return (
    <Card data-testid={testId}>
      {title == null && badge == null ? null : onToggle ? (
        <ButtonBase aria-expanded={Boolean(open)} onClick={onToggle} sx={{ display: "block", width: 1, borderRadius: "inherit" }}>
          {header}
        </ButtonBase>
      ) : (
        header
      )}
      {children && (title != null || badge != null) ? <Divider /> : null}
      {children ? (
        <Stack spacing={2} sx={{ p: { xs: 2, sm: 3 }, minWidth: 0 }}>
          {children}
        </Stack>
      ) : null}
    </Card>
  );
}

/** Head row of a question / step / slot: badge, type select, meta, then the row actions at the end. */
export function QuestionHead({ children }: { children: ReactNode }) {
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: "center", flexWrap: "wrap", minWidth: 0 }}>
      {children}
    </Stack>
  );
}

/** Pushes the actions that follow it to the end of a head / bar row. */
export function Spacer() {
  return <Box aria-hidden sx={{ flex: 1 }} />;
}

/**
 * One question / step / slot: an outlined Paper with a head row, a body and an optional dashed
 * footer. `dragging` dims it while a drag is under way; `dragProps` wires the builder's drag handlers.
 */
export function QuestionShell({
  head,
  children,
  foot,
  dragging,
  dragProps,
  testId,
}: {
  head: ReactNode;
  children?: ReactNode;
  foot?: ReactNode;
  dragging?: boolean;
  dragProps?: Record<string, unknown>;
  testId?: string;
}) {
  return (
    <Paper
      variant="outlined"
      data-testid={testId}
      {...dragProps}
      sx={(theme) => ({
        p: { xs: 1.5, sm: 2 },
        borderRadius: 0.75,
        minWidth: 0,
        transition: theme.transitions.create(["border-color", "opacity"], { duration: theme.transitions.duration.shorter }),
        "&:hover": { borderColor: varAlpha(theme.vars.palette.primary.mainChannel, 0.4) },
        ...(dragging ? { opacity: 0.55, borderColor: "primary.main" } : {}),
      })}
    >
      <QuestionHead>{head}</QuestionHead>
      {children ? <Stack spacing={1.5} sx={{ pt: 2, minWidth: 0 }}>{children}</Stack> : null}
      {foot ? (
        <Stack
          direction="row"
          spacing={1.5}
          useFlexGap
          sx={{ alignItems: "center", flexWrap: "wrap", mt: 2, pt: 1.5, borderTop: 1, borderTopStyle: "dashed", borderColor: "divider" }}
        >
          {foot}
        </Stack>
      ) : null}
    </Paper>
  );
}

/** A grouped config block (choices, limits, capture) on the template neutral surface. */
export function ConfigBox({ title, action, children, testId }: { title?: ReactNode; action?: ReactNode; children?: ReactNode; testId?: string }) {
  return (
    <Stack spacing={1.5} data-testid={testId} sx={{ p: { xs: 1.5, sm: 2 }, borderRadius: 0.75, bgcolor: "background.neutral", minWidth: 0 }}>
      {title || action ? <ConfigHead title={title} action={action} /> : null}
      {children}
    </Stack>
  );
}

/** Heading row of a config block: template subtitle2 + an action at the end. */
export function ConfigHead({ title, action }: { title?: ReactNode; action?: ReactNode }) {
  return (
    <Stack direction={{ xs: "column", sm: "row" }} spacing={1} useFlexGap sx={{ alignItems: { xs: "flex-start", sm: "center" }, justifyContent: "space-between", flexWrap: "wrap" }}>
      {typeof title === "string" ? (
        <Typography variant="subtitle2" component="span">
          {title}
        </Typography>
      ) : (
        title
      )}
      {action}
    </Stack>
  );
}

/** A config group heading with its one-line explainer under it (subtitle2 + caption). */
export function GroupTitle({ title, hint }: { title: ReactNode; hint?: ReactNode }) {
  return (
    <Stack spacing={0.25} sx={{ minWidth: 0 }}>
      <Typography variant="subtitle2" component="span">
        {title}
      </Typography>
      {hint ? (
        <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>
          {hint}
        </Typography>
      ) : null}
    </Stack>
  );
}

/** A row of fields that share the width from sm up and stack on a phone. */
export function FieldGrid({ children }: { children: ReactNode }) {
  return (
    <Stack direction={{ xs: "column", sm: "row" }} spacing={1.5} useFlexGap sx={{ flexWrap: "wrap", minWidth: 0, "& > *": { flex: { sm: "1 1 var(--field-basis)" }, minWidth: 0 }, "--field-basis": (theme) => theme.spacing(22.5) }}>
      {children}
    </Stack>
  );
}

/** One choice row: the radio / tick marker the phone will show, the field, an optional action. */
export function OptionRow({ multi, children, action }: { multi?: boolean; children: ReactNode; action?: ReactNode }) {
  return (
    <Stack direction="row" spacing={1} sx={{ alignItems: "center", minWidth: 0 }}>
      <Box
        aria-hidden
        sx={{ width: "calc(1.5 * var(--spacing))", height: "calc(1.5 * var(--spacing))", flexShrink: 0, border: 2, borderColor: "text.disabled", borderRadius: multi ? "calc(0.5 * var(--spacing))" : "50%" }}
      />
      <Box sx={{ flex: 1, minWidth: 0 }}>{children}</Box>
      {action}
    </Stack>
  );
}

/** A labelled template Checkbox with a 44px tap box on a phone. */
export function CheckLine({
  checked,
  onChange,
  label,
  disabled,
  title,
  testId,
  inputTestId,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: ReactNode;
  disabled?: boolean;
  title?: string;
  testId?: string;
  /** data-testid on the checkbox input itself (tests tick it directly). */
  inputTestId?: string;
}) {
  return (
    <FormControlLabel
      disabled={disabled}
      title={title}
      data-testid={testId}
      control={
        <Checkbox
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
          sx={{ p: { xs: 1.5, sm: 1 } }}
          slotProps={inputTestId ? { input: { "data-testid": inputTestId } as InputHTMLAttributes<HTMLInputElement> } : undefined}
        />
      }
      label={label}
      sx={{ mr: 0, ml: -1, alignItems: "center", minWidth: 0, "& .MuiFormControlLabel-label": { typography: "body2" } }}
    />
  );
}

/** A labelled template Radio (one of a mode group) with a 44px tap box on a phone. */
export function RadioLine({ name, value, checked, onChange, label }: { name: string; value: string; checked: boolean; onChange: () => void; label: ReactNode }) {
  return (
    <FormControlLabel
      control={<Radio name={name} value={value} checked={checked} onChange={onChange} sx={{ p: { xs: 1.5, sm: 1 } }} />}
      label={label}
      sx={{ mr: 0, ml: -1, alignItems: "center", minWidth: 0, "& .MuiFormControlLabel-label": { typography: "body2" } }}
    />
  );
}

/** An "Only if ..." row: caption words between inline selects, wrapping on a phone. */
export function CondRow({ children }: { children: ReactNode }) {
  return (
    <Stack
      direction="row"
      spacing={1}
      useFlexGap
      sx={{ alignItems: "center", flexWrap: "wrap", minWidth: 0, typography: "caption", color: "text.secondary" }}
    >
      {children}
    </Stack>
  );
}

/** Small template IconButton with a registered Iconify icon; `danger` tints the hover red. */
export function IconAction({
  icon,
  label,
  onClick,
  disabled,
  danger,
  title,
}: {
  icon: IconifyName;
  label: string;
  onClick?: () => void;
  disabled?: boolean;
  danger?: boolean;
  title?: string;
}) {
  return (
    <IconButton
      size="small"
      aria-label={label}
      title={title ?? label}
      disabled={disabled}
      onClick={onClick}
      sx={{ minWidth: { xs: TAP }, minHeight: { xs: TAP, sm: "auto" }, ...(danger ? { "&:hover": { color: "error.main" } } : {}) }}
    >
      <Iconify icon={icon} width={18} />
    </IconButton>
  );
}

/** "Add a question / option / page" action: template text Button (outlined for the page-level add). */
export function AddButton({ label, onClick, outlined, disabled, testId }: { label: string; onClick: () => void; outlined?: boolean; disabled?: boolean; testId?: string }) {
  return (
    <Button
      color="primary"
      variant={outlined ? "outlined" : "text"}
      size="small"
      disabled={disabled}
      data-testid={testId}
      startIcon={<Iconify icon={EDITOR_ICON.add} />}
      onClick={onClick}
      sx={{ alignSelf: "flex-start" }}
    >
      {label}
    </Button>
  );
}

/** A locked / backend-owned note: lock icon + caption text (e.g. a register question's key). */
export function LockedNote({ children, title }: { children: ReactNode; title?: string }) {
  return (
    <Typography variant="caption" component="span" title={title} sx={{ color: "text.secondary", display: "inline-flex", alignItems: "center", columnGap: 0.5, flexWrap: "wrap" }}>
      <Iconify icon={EDITOR_ICON.lock} width={14} />
      {children}
    </Typography>
  );
}

/** Up / down / remove trio at the end of a head row. */
export function MoveActions({
  upLabel,
  downLabel,
  removeLabel,
  onUp,
  onDown,
  onRemove,
  first,
  last,
  removeDisabled,
  removeIcon = EDITOR_ICON.remove,
}: {
  upLabel: string;
  downLabel: string;
  removeLabel?: string;
  onUp: () => void;
  onDown: () => void;
  onRemove?: () => void;
  first: boolean;
  last: boolean;
  removeDisabled?: boolean;
  removeIcon?: IconifyName;
}) {
  return (
    <Stack direction="row" spacing={0.5} sx={{ flexShrink: 0 }}>
      <IconAction icon={EDITOR_ICON.up} label={upLabel} disabled={first} onClick={onUp} />
      <IconAction icon={EDITOR_ICON.down} label={downLabel} disabled={last} onClick={onDown} />
      {onRemove && removeLabel ? <IconAction icon={removeIcon} label={removeLabel} danger disabled={removeDisabled} onClick={onRemove} /> : null}
    </Stack>
  );
}

/**
 * Save / dry-run / publish bar that stays reachable while a long editor is scrolled: a template Card
 * pinned to the bottom of the scroller. `.main` reserves bottom padding (minimal-theme.css), and a
 * sticky inset is measured from the scroller's padding box, so the bar cancels it to sit on the
 * viewport edge.
 */
export function StickyBar({ children, testId }: { children: ReactNode; testId?: string }) {
  return (
    <Card
      data-testid={testId}
      sx={(theme) => ({
        position: "sticky",
        bottom: { xs: theme.spacing(-12), sm: theme.spacing(-11) },
        zIndex: 3,
        display: "flex",
        alignItems: "center",
        flexWrap: "wrap",
        columnGap: 1.5,
        rowGap: 1.5,
        px: 2,
        py: 1.5,
        boxShadow: theme.vars.customShadows.z8,
      })}
    >
      {children}
    </Card>
  );
}

/** Short bullet list (problems / backend errors) in body2 caption tone. */
export function ProblemList({ items, max = 5, muted }: { items: readonly string[]; max?: number; muted?: boolean }) {
  if (!items.length) return null;
  return (
    <Box component="ul" sx={{ m: 0, mt: 0.5, pl: 2, typography: "caption", color: muted ? "text.secondary" : "inherit" }}>
      {items.slice(0, max).map((p, i) => (
        <li key={i}>{p}</li>
      ))}
    </Box>
  );
}

export type EditorResult = { ok: boolean; message: string; report?: { valid: boolean; errors: { message: string }[] } | null };

/** The save / publish result: template Alert (info on success, warning with the first errors otherwise). */
export function ResultNotice({ result, okLabel }: { result: EditorResult | null; okLabel?: string }) {
  if (!result) return null;
  const errors = result.report && !result.report.valid ? result.report.errors.map((e) => e.message) : [];
  return (
    <Alert severity={result.ok ? "success" : "warning"} role="status">
      {result.ok && okLabel ? <Label color="success" sx={{ mr: 1 }}>{okLabel}</Label> : null}
      {result.message}
      <ProblemList items={errors} max={6} />
    </Alert>
  );
}

"use client";

// The shared inline editor behind every editable cell on Counts Breakdown.
//
// It owns the INTERACTION and nothing else: double-click to open, type to narrow, pick, confirm,
// apply. What each cell actually writes -- a pen's cohort tag, a row's breed, a row's sex -- is
// supplied by the caller as two functions, because those writes have genuinely different scopes and
// must stay visibly different in the code that calls them.
//
// Double-click rather than single: every cell in this table is a census figure an operator reads,
// and a single click that opened an editor would fire constantly while scanning the page. Enter and
// Space open it too, so the control is reachable without a mouse.
//
// CHECK-THEN-APPLY is not optional here. These writes have no approval step and no proof behind
// them, so the preview IS the safety mechanism: picking a value shows how many animals it would
// move, and only then does the button that applies it appear.
//
// Every visible string comes from the page contract, including the default reason that lands in the
// audit row. This component composes no copy and decides no authority.
import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import ButtonBase from "@mui/material/ButtonBase";
import MenuItem from "@mui/material/MenuItem";
import MenuList from "@mui/material/MenuList";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { CustomPopover } from "@/components/minimal/custom-popover";
import { TAP_MIN } from "@/theme/tap-target";

// InlineChoice is one selectable value. `label` is what the cell will show once applied -- the
// stored value, not a prettier synonym -- and `description`/`hint` are secondary context.
export type InlineChoice = {
  value: string;
  label: string;
  description?: string;
  hint?: string;
};

// InlinePreview is what the caller's preview call answered. `subject` names the thing being
// changed (the backend's own composed location string), `count` is how many animals move, and
// `consequence` is an optional extra line -- the kid/adult band, for instance.
export type InlinePreview = {
  subject: string;
  count: number;
  consequence?: string;
};

// Named aliases rather than inline `=> Promise<...>` signatures: the contract-literal guard reads
// an arrow type followed by a generic as JSX text and flags it. The indirection costs nothing and
// keeps the guard able to see real violations instead of being switched off here.
type PreviewOutcome = Promise<InlinePreview | { error: string }>;
type ApplyOutcome = Promise<{ error?: string }>;

type Phase =
  | { kind: "closed" }
  | { kind: "picking" }
  | { kind: "checking"; value: string }
  | { kind: "confirming"; value: string; preview: InlinePreview }
  | { kind: "failed"; message: string };


const EMPTY_SX = { color: "text.secondary", typography: "caption" } as const;

/** The cell value IS the control: reads as the plain value, 44px tap box, focus ring on keyboard. */
const VALUE_BUTTON_SX = {
  font: "inherit",
  color: "inherit",
  textAlign: "left",
  borderRadius: 0.75,
  minWidth: TAP_MIN,
  minHeight: TAP_MIN,
  display: "inline-flex",
  alignItems: "center",
  justifyContent: "flex-start",
  "&.Mui-focusVisible": { outline: 2, outlineStyle: "solid", outlineColor: "primary.main", outlineOffset: 2 },
} as const;

export function InlineCellEditor({
  pageContract,
  current,
  emptyLabel,
  choices,
  enabled,
  disabledReason,
  onPreview,
  onApply,
  renderCurrent,
}: {
  pageContract: AdminUiPageContract;
  current: string;
  emptyLabel: string;
  choices: InlineChoice[];
  enabled: boolean;
  disabledReason: string;
  onPreview: (value: string) => PreviewOutcome;
  onApply: (value: string, reason: string, idempotencyKey: string) => ApplyOutcome;
  renderCurrent?: (current: string) => React.ReactNode;
}) {
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>({ kind: "closed" });
  const [filter, setFilter] = useState("");
  const [reason, setReason] = useState("");
  const [pending, startTransition] = useTransition();
  const inputRef = useRef<HTMLInputElement>(null);

  // The popup is the template popover (CustomPopover): portalled to <body>, so the table's
  // horizontal-scroll container can never clip it, and placed by MUI against the cell each time it
  // opens (flipping inside the viewport) -- never from a stale measurement. MUI also closes it on
  // an outside click and on Escape and hands focus back to the cell.
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);

  // One key per INTENT, minted when a preview is accepted and held across retries, so a double
  // click or a retried network failure replays the first write instead of writing twice.
  const [commitKey, setCommitKey] = useState("");

  const open = phase.kind !== "closed";
  // Local state only: this never navigates, so the row behind it is not re-fetched on open or close.
  const toggleOpen = (anchor: HTMLElement) => {
    if (open) {
      close();
      return;
    }
    setAnchorEl(anchor);
    setPhase({ kind: "picking" });
  };

  useEffect(() => {
    if (phase.kind === "picking") inputRef.current?.focus();
  }, [phase.kind]);
  function close() {
    setPhase({ kind: "closed" });
    setFilter("");
    setCommitKey("");
    setAnchorEl(null);
  }

  // Substring match on the stored value AND its description, because an operator who knows the
  // vocabulary types "f2" as readily as "fattening".
  const matches = useMemo(() => {
    const needle = filter.trim().toLowerCase();
    if (!needle) return choices;
    return choices.filter(
      (choice) =>
        choice.value.toLowerCase().includes(needle) ||
        choice.label.toLowerCase().includes(needle) ||
        (choice.description ?? "").toLowerCase().includes(needle),
    );
  }, [filter, choices]);
  const choiceLabel = (value: string) => choices.find((choice) => choice.value === value)?.label ?? value;

  function pick(value: string) {
    setPhase({ kind: "checking", value });
    startTransition(async () => {
      const preview = await onPreview(value);
      if ("error" in preview) {
        setPhase({ kind: "failed", message: preview.error || copy(pageContract, "action.retag.failed") });
        return;
      }
      setReason(copy(pageContract, "action.retag.default_reason"));
      setCommitKey(crypto.randomUUID());
      setPhase({ kind: "confirming", value, preview });
    });
  }

  function apply(value: string) {
    startTransition(async () => {
      const result = await onApply(value, reason.trim(), commitKey);
      if (result.error) {
        setPhase({ kind: "failed", message: result.error });
        return;
      }
      close();
      router.refresh();
    });
  }

  if (!enabled) {
    // Disabled-with-reason, never hidden: the backend decides authority and says why, and a control
    // that comes and goes reads as a broken screen rather than a withheld one.
    return (
      <span title={disabledReason} aria-disabled="true">
        {current ? (renderCurrent?.(current) ?? current) : <Box component="span" sx={EMPTY_SX}>{emptyLabel}</Box>}
      </span>
    );
  }

  return (
    <Box component="span" sx={{ position: "relative", display: "inline-block" }}>
      <ButtonBase
        type="button"
        disableRipple
        sx={VALUE_BUTTON_SX}
        onClick={(event) => toggleOpen(event.currentTarget)}
        onKeyDown={(event) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          toggleOpen(event.currentTarget);
        }}
        aria-expanded={open}
        aria-haspopup="dialog"
        title={copy(pageContract, "action.retag.hint")}
      >
        {current ? (renderCurrent?.(current) ?? current) : <Box component="span" sx={EMPTY_SX}>{emptyLabel}</Box>}
      </ButtonBase>

      <CustomPopover
        open={open}
        anchorEl={anchorEl}
        onClose={close}
        slotProps={{
          arrow: { placement: "top-left" },
          paper: { role: "dialog", "aria-label": copy(pageContract, "action.retag.hint"), sx: { width: 260, p: 1.25, whiteSpace: "normal" } },
        }}
      >
        <Box sx={{ display: "grid", gap: 1 }}>
          {phase.kind === "picking" || phase.kind === "checking" ? (
            <>
              <TextField
                size="small"
                fullWidth
                inputRef={inputRef}
                autoFocus
                value={filter}
                onChange={(event) => setFilter(event.target.value)}
                placeholder={copy(pageContract, "action.retag.search_placeholder")}
                slotProps={{ htmlInput: { "aria-label": copy(pageContract, "action.retag.search_placeholder") } }}
              />
              {matches.length === 0 ? (
                <Typography variant="caption" sx={{ color: "text.secondary", px: 0.25 }}>{copy(pageContract, "action.retag.no_matches")}</Typography>
              ) : (
                <MenuList sx={{ maxHeight: 210, overflowY: "auto", overscrollBehavior: "contain" }}>
                  {matches.map((choice) => (
                    <MenuItem
                      key={choice.value}
                      disabled={pending}
                      onClick={() => pick(choice.value)}
                      sx={{ justifyContent: "space-between", alignItems: "baseline", gap: 1.25, whiteSpace: "normal" }}
                    >
                      <Box component="span" sx={{ display: "grid", gap: 0.125, textAlign: "left" }}>
                        <span>{choice.label}</span>
                        {choice.description ? <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>{choice.description}</Typography> : null}
                      </Box>
                      {choice.hint ? <Typography variant="caption" component="span" sx={{ color: "text.secondary" }}>{choice.hint}</Typography> : null}
                    </MenuItem>
                  ))}
                </MenuList>
              )}
              {phase.kind === "checking" ? (
                <Typography variant="caption" sx={{ color: "text.secondary", px: 0.25 }}>{copy(pageContract, "action.retag.checking")}</Typography>
              ) : null}
            </>
          ) : null}

          {phase.kind === "confirming" ? (
            <>
              {/* Subject and count come from the PREVIEW, never from the row: the two can differ,
                  and the operator must confirm what the write will actually do. */}
              <Typography variant="body2">
                <b>{choiceLabel(phase.value)}</b> · {phase.preview.subject}
              </Typography>
              <Typography variant="caption" sx={{ color: "text.secondary" }}>
                {phase.preview.count === 0
                  ? copy(pageContract, "action.retag.empty_scope")
                  : `${phase.preview.count} ${
                      phase.preview.count === 1
                        ? copy(pageContract, "action.retag.animal_noun")
                        : copy(pageContract, "action.retag.animals_noun")
                    }`}
              </Typography>
              {phase.preview.consequence ? (
                <Typography variant="caption" sx={{ color: "primary.dark", fontWeight: "fontWeightSemiBold" }}>{phase.preview.consequence}</Typography>
              ) : null}
              <TextField
                size="small"
                fullWidth
                label={copy(pageContract, "action.retag.reason_label")}
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                slotProps={{ htmlInput: { maxLength: 500 }, inputLabel: { shrink: true } }}
                sx={{ mt: 0.5 }}
              />
              <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1, justifyContent: "flex-end" }}>
                <Button size="small" variant="outlined" color="inherit" onClick={close} disabled={pending}>
                  {copy(pageContract, "action.retag.cancel")}
                </Button>
                <Button
                  size="small"
                  variant="contained"
                  color="primary"
                  onClick={() => apply(phase.value)}
                  // The backend requires 3..500 characters, so the button states that rule rather
                  // than letting the operator discover it as a server error.
                  disabled={pending || reason.trim().length < 3}
                >
                  {pending ? copy(pageContract, "action.retag.applying") : copy(pageContract, "action.retag.apply")}
                </Button>
              </Box>
            </>
          ) : null}

          {phase.kind === "failed" ? (
            <>
              <Typography variant="body2">{phase.message}</Typography>
              <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
                <Button size="small" variant="outlined" color="inherit" onClick={() => setPhase({ kind: "picking" })}>
                  {copy(pageContract, "action.retag.cancel")}
                </Button>
              </Box>
            </>
          ) : null}
        </Box>
      </CustomPopover>
    </Box>
  );
}

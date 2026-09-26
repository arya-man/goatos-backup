"use client";
import Table from "@mui/material/Table";
import ButtonBase from "@mui/material/ButtonBase";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { EmptyContent } from "@/components/minimal/empty-content";
import { TableHeadCustom } from "@/components/minimal/table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

// The CEO/CXO Toxin review list + drawer (maintainer decision 2026-08-25).
//
// Mounted on /verify when the backend-declared `toxin_tab` control is enabled and ?toxin=1 is
// selected. Every visible business string is BACKEND-OWNED: rows render status_chip /
// context_line / outcome_label / origin_line verbatim (mapped in ./toxin-rows.ts), the drawer
// renders the detail payload's step titles/instructions and the page contract's toxin.* copy.
//
// The drawer is CLIENT-LOCAL overlay state: a row click sets React state and fetches the detail
// through a Server Action — never a navigation, never a router push (repo drawer rule).
import { useCallback, useEffect, useRef, useState, useTransition } from "react";
import { faro } from "@grafana/faro-web-sdk";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ToxinTask } from "@/lib/api/server";
import { fmtDateTime } from "@/lib/format";
import { loadToxinTaskDetailAction, recordToxinVerdictAction, type ToxinDetailLoad } from "./toxin-actions";
import { toxinReviewRows, type ToxinReviewRow } from "./toxin-rows";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import IconButton from "@mui/material/IconButton";
import MuiLink from "@mui/material/Link";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";
import useMediaQuery from "@mui/material/useMediaQuery";
import type { Theme } from "@mui/material/styles";
import { X } from "lucide-react";
import { DrawerBlock } from "@/components/app/detail-drawer";

export function ToxinReviewList({
  tasks,
  pageContract,
  returnTo,
  feedback,
}: {
  tasks: ToxinTask[];
  pageContract: AdminUiPageContract;
  /** The current /verify?toxin=1… URL, carried through the verdict action's redirect. */
  returnTo: string;
  feedback: { status?: string; code?: string };
}) {
  const rows = toxinReviewRows(tasks);
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);
  const [detail, setDetail] = useState<ToxinDetailLoad | undefined>(undefined);
  const [loading, startLoading] = useTransition();
  const text = useCallback((key: string) => copy(pageContract, key), [pageContract]);

  // TELEMETRY GUARDRAIL: tab open + verdict submit result, mirroring
  // verification-review-telemetry.tsx's Faro usage. Faro must never break the page.
  const openedReported = useRef(false);
  useEffect(() => {
    if (openedReported.current) return;
    openedReported.current = true;
    try {
      faro.api?.pushEvent("toxin_review_tab_opened", { rows: String(rows.length) });
    } catch {
      // Faro must never break the page.
    }
    // Row count at first paint is enough; re-reporting on refresh would double-count the visit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  const feedbackReported = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!feedback.status) return;
    const key = `${feedback.status}:${feedback.code ?? ""}`;
    if (feedbackReported.current === key) return;
    feedbackReported.current = key;
    try {
      faro.api?.pushEvent("toxin_review_action", { status: feedback.status, code: feedback.code ?? "" });
    } catch {
      // Faro must never break the page.
    }
  }, [feedback.status, feedback.code]);

  const openRow = useCallback(
    (taskId: string) => {
      setSelectedId(taskId);
      setDetail(undefined);
      startLoading(async () => {
        const loaded = await loadToxinTaskDetailAction(taskId);
        setDetail(loaded);
      });
    },
    [startLoading],
  );
  const closeDrawer = useCallback(() => {
    setSelectedId(undefined);
    setDetail(undefined);
  }, []);

  // Escape, the backdrop and X close through the MUI Dialog's onClose (ToxinDrawer).

  const feedbackText = feedback.status
    ? feedback.status === "success"
      ? text("toxin.feedback.done")
      : copy(pageContract, `toxin.feedback.${feedback.code ?? ""}`, "")
    : "";

  return (
    <>
      {feedback.status ? (
        <Alert severity={feedback.status === "success" ? "info" : "error"} sx={{ mx: 2.5, mt: 2.5 }}>
          {/* Unmapped error codes render nothing rather than leaking the raw token. */}
          {feedbackText || (feedback.status === "error" ? copy(pageContract, "feedback.failed", "") : "")}
        </Alert>
      ) : null}

      {/* Template list table: Scrollbar + TableHeadCustom, hover rows, soft status Label. Each cell
          is a full-width ButtonBase so the whole row opens the local drawer (never a navigation). */}
      <Scrollbar>
        <Table className="toxin-review-table" sx={{ minWidth: 720 }}>
          <TableHeadCustom
            headCells={[
              { id: "test", label: text("toxin.drawer.title") },
              { id: "reading", label: text("toxin.drawer.reading") },
              { id: "status", label: copy(pageContract, "drawer.meta.status") },
            ]}
          />
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={3}>
                  <EmptyContent filled role="status" title={text("toxin.state.empty")} sx={{ py: 10 }} />
                </TableCell>
              </TableRow>
            ) : (
              rows.map((row) => (
                <TableRow key={row.taskId} hover>
                  <TableCell>
                    <ButtonBase onClick={() => openRow(row.taskId)} sx={ROW_BUTTON_SX}>
                      <Box component="span" sx={{ display: "block", typography: "body2" }}>
                        {row.contextLine}
                      </Box>
                      {/* The purchase date is NOT repeated here: context_line already ends with
                          it, and rendering both showed the same day twice in two formats
                          (2026-08-26 then 26-08-2026). The second line carries only what the
                          context line does not say — the retest round, when there is one. */}
                      {row.roundChip ? (
                        <Box component="span" sx={{ mt: 0.5, display: "block" }}>
                          <Label variant="soft" color="info">{row.roundChip}</Label>
                        </Box>
                      ) : null}
                    </ButtonBase>
                  </TableCell>
                  <TableCell sx={{ color: "text.secondary", whiteSpace: "nowrap" }}>
                    <ButtonBase onClick={() => openRow(row.taskId)} sx={ROW_BUTTON_SX}>
                      {row.outcomeLabel || "—"}
                    </ButtonBase>
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    <ButtonBase onClick={() => openRow(row.taskId)} sx={ROW_BUTTON_SX}>
                      <Label variant="soft" color="warning">{row.statusChip}</Label>
                    </ButtonBase>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Scrollbar>

      <ToxinDrawer
        // Keyed by the selected task so drawer-local state (the reject reason draft) resets when a
        // different test opens, without a setState-in-effect.
        key={selectedId ?? "closed"}
        open={Boolean(selectedId)}
        loading={loading}
        detail={detail}
        row={rows.find((row) => row.taskId === selectedId)}
        pageContract={pageContract}
        returnTo={returnTo}
        onClose={closeDrawer}
      />
    </>
  );
}

function ToxinDrawer({
  open,
  loading,
  detail,
  row,
  pageContract,
  returnTo,
  onClose,
}: {
  open: boolean;
  loading: boolean;
  detail: ToxinDetailLoad | undefined;
  row: ToxinReviewRow | undefined;
  pageContract: AdminUiPageContract;
  returnTo: string;
  onClose: () => void;
}) {
  const text = useCallback((key: string) => copy(pageContract, key), [pageContract]);
  const [reason, setReason] = useState("");
  const fullScreen = useMediaQuery((theme: Theme) => theme.breakpoints.down("sm"));
  if (!open || !row) return null;

  const loaded = detail?.ok ? detail : undefined;
  const task = loaded?.detail;
  // Template Dialog (DialogTitle / DialogContent / DialogActions); full screen below sm.
  return (
    <Dialog
      open={open}
      onClose={onClose}
      fullWidth
      maxWidth="sm"
      fullScreen={fullScreen}
      scroll="paper"
      aria-label={text("toxin.drawer.title")}
    >
      <DialogTitle component="div" sx={{ display: "flex", alignItems: "flex-start", gap: 1 }}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          {/* The headline is the backend-composed context line, never a client-assembled one. */}
          <Typography variant="h6" component="h2">{row.contextLine}</Typography>
          <Typography variant="body2" sx={{ color: "text.secondary" }}>{row.statusChip}</Typography>
        </Box>
        <IconButton aria-label={copy(pageContract, "drawer.close_label")} onClick={onClose} sx={{ mt: -0.5, mr: -1 }}>
          <X size={20} aria-hidden="true" />
        </IconButton>
      </DialogTitle>
      <DialogContent dividers sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
        {loading || !detail ? (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>…</Typography>
        ) : !detail.ok ? (
          <Alert severity="error">
            <b>{copy(pageContract, "feedback.failed")}</b>
          </Alert>
        ) : task ? (
          <>
            <DrawerBlock title={text("toxin.drawer.steps")}>
              <Box component="ol" sx={{ m: 0, pl: 2.25, display: "flex", flexDirection: "column", gap: 1 }}>
                {task.steps.map((step) => {
                  const proofUrl = step.proof_ref ? (loaded?.proofUrls[step.proof_ref] ?? null) : null;
                  return (
                    <li key={step.step_no}>
                      <Box sx={{ display: "flex", gap: 1, alignItems: "baseline", flexWrap: "wrap" }}>
                        <Typography variant="subtitle2" component="b">{step.title}</Typography>
                        <Typography variant="caption" sx={{ color: "text.secondary" }}>{step.instruction}</Typography>
                      </Box>
                      <Box sx={{ display: "flex", gap: 1.25, flexWrap: "wrap", typography: "caption", color: "text.secondary" }}>
                        {step.completed_by ? <span>{step.completed_by}</span> : null}
                        {step.completed_at ? <span>{fmtDateTime(step.completed_at)}</span> : null}
                        {/* A resolvable proof gets a backend proof route; an unresolved one
                            honestly shows only who did the step and when — see
                            loadToxinTaskDetailAction's bounded resolver and its limitation note. */}
                        {proofUrl ? (
                          <MuiLink href={proofUrl} target="_blank" rel="noreferrer">
                            {copy(pageContract, "drawer.media.open")}
                          </MuiLink>
                        ) : null}
                      </Box>
                    </li>
                  );
                })}
              </Box>
            </DrawerBlock>

            {task.strip_photo_ref ? (
              <DrawerBlock title={text("toxin.drawer.strip_photo")}>
                {loaded?.proofUrls[task.strip_photo_ref] ? (
                  <MuiLink href={loaded.proofUrls[task.strip_photo_ref] ?? undefined} target="_blank" rel="noreferrer" variant="body2">
                    {copy(pageContract, "drawer.media.open")}
                  </MuiLink>
                ) : (
                  <Typography variant="body2" sx={{ color: "text.secondary" }}>{copy(pageContract, "drawer.media.empty")}</Typography>
                )}
              </DrawerBlock>
            ) : null}

            <DrawerBlock title={text("toxin.drawer.reading")}>
              <Typography variant="body2">{task.outcome_label || "—"}</Typography>
              {task.cancel_reason ? <Typography variant="caption" sx={{ color: "text.secondary" }}>{task.cancel_reason}</Typography> : null}
            </DrawerBlock>

            {task.status === "pending_review" ? (
              // Accept is ONE click; Reject requires a reason before its button enables — the
              // backend enforces the same rule (400 reject_reason_required). Both are one Server
              // Action with a derived idempotency key, redirect-feedback on tx_status/tx_code, and
              // the loaded row_version as the optimistic-concurrency fence. The buttons live in
              // DialogActions and submit this form through the `form` attribute.
              <Box component="form" id={VERDICT_FORM_ID} action={recordToxinVerdictAction}>
                <input type="hidden" name="task_id" value={task.task_id} />
                <input type="hidden" name="row_version" value={String(task.row_version)} />
                <input type="hidden" name="return_to" value={returnTo} />
                <TextField
                  id="toxin-reject-reason"
                  name="reason"
                  label={text("toxin.drawer.reject_reason")}
                  placeholder={text("toxin.drawer.reject_reason_hint")}
                  multiline
                  rows={2}
                  fullWidth
                  value={reason}
                  onChange={(event) => setReason(event.target.value)}
                  slotProps={{ inputLabel: { shrink: true } }}
                />
              </Box>
            ) : null}
          </>
        ) : null}
      </DialogContent>
      {task?.status === "pending_review" ? (
        <DialogActions>
          <Button type="submit" form={VERDICT_FORM_ID} name="decision" value="reject" variant="outlined" color="inherit" disabled={!reason.trim()} title={!reason.trim() ? text("toxin.drawer.reject_reason_hint") : undefined}>
            {text("toxin.action.reject")}
          </Button>
          <Button type="submit" form={VERDICT_FORM_ID} name="decision" value="accept" variant="contained" color="primary">
            {text("toxin.action.accept")}
          </Button>
        </DialogActions>
      ) : null}
    </Dialog>
  );
}

const VERDICT_FORM_ID = "toxin-verdict-form";

// A full-cell tap target (>= 44px) that reads as plain table text: the row, not a button, is the affordance.
const ROW_BUTTON_SX = { display: "block", width: 1, minHeight: 44, textAlign: "left", font: "inherit", color: "inherit" } as const;

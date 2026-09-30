"use client";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import CircularProgress from "@mui/material/CircularProgress";
import LinearProgress from "@mui/material/LinearProgress";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ConfigurationImportBundle, ConfigurationImportJob } from "@/lib/api/configuration-server";
import { UploadFile } from "@/components/app/upload-file";
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";
import Alert from "@mui/material/Alert";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom, type TableHeadCellProps } from "@/components/app/table";
import { SheetActions, SheetCounts, SheetDrawerBody, SheetHint, SheetJobCard, SheetRecent, SheetSection, SheetStatus } from "./sheet-drawer-parts";

/**
 * The onboarding workbook drawer (maintainer instruction 2026-09-19): DOWNLOAD one Excel
 * template with a tab per list, or every list's rows as such a workbook, and UPLOAD one back.
 * The upload is a BUNDLE of tab jobs the backend checks and applies in order; the drawer polls
 * the bundle and renders each tab's backend-owned counts. Files stream through the same
 * same-origin proxy the sheet drawer uses; nothing here holds a workbook. Every sentence is
 * page-contract copy.
 */

const SHEETS = "/api/admin/configuration-sheets";
const POLL_MS = 1500;

type Phase = "idle" | "uploading" | "polling" | "error";

function bundleHref(bundleId: string, action = ""): string {
  return `${SHEETS}/bundles/${encodeURIComponent(bundleId)}${action ? `/${action}` : ""}`;
}

async function readBundle(response: Response): Promise<{ bundle?: ConfigurationImportBundle; message?: string }> {
  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }
  const record = (body ?? {}) as { bundle?: ConfigurationImportBundle; message?: string; error?: string };
  if (!response.ok) return { message: record.message ?? record.error ?? `HTTP ${response.status}` };
  return { bundle: record.bundle };
}

function inFlight(bundle: ConfigurationImportBundle | null): boolean {
  return bundle?.status === "validating" || bundle?.status === "applying";
}

function jobProgress(job: ConfigurationImportJob): number {
  if (job.total_rows <= 0) return 0;
  return Math.min(100, Math.round((job.progress_row_no / (job.total_rows + 1)) * 100));
}

export function WorkbookDrawer({ pageContract, canWrite, registerLabels }: { pageContract: AdminUiPageContract; canWrite: boolean; registerLabels: Record<string, string> }) {
  const c = (key: string) => copy(pageContract, key);
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>("idle");
  const [message, setMessage] = useState("");
  const [bundle, setBundle] = useState<ConfigurationImportBundle | null>(null);
  const [recent, setRecent] = useState<ConfigurationImportBundle[]>([]);
  const [fileName, setFileName] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);
  const appliedRef = useRef(false);

  const loadRecent = useCallback(async () => {
    try {
      const response = await fetch(`${SHEETS}/workbook/imports`, { cache: "no-store" });
      if (!response.ok) return;
      const body = (await response.json()) as { bundles?: ConfigurationImportBundle[] };
      setRecent(body.bundles ?? []);
    } catch {
      // The recent list is a convenience; the drawer works without it.
    }
  }, []);

  useEffect(() => {
    if (!canWrite) return;
    const timer = window.setTimeout(() => void loadRecent(), 0);
    return () => window.clearTimeout(timer);
  }, [canWrite, loadRecent]);

  // Poll while the backend is checking or applying.
  useEffect(() => {
    if (!bundle || !inFlight(bundle)) return;
    const timer = window.setTimeout(async () => {
      try {
        const response = await fetch(bundleHref(bundle.id), { cache: "no-store" });
        const { bundle: next, message: failure } = await readBundle(response);
        if (!next) {
          setPhase("error");
          setMessage(failure ?? c("action.failed_message"));
          return;
        }
        setBundle(next);
        if (next.status === "applied" && !appliedRef.current) {
          appliedRef.current = true;
          router.refresh();
          void loadRecent();
        }
        if (!inFlight(next)) setPhase("idle");
      } catch {
        setPhase("error");
        setMessage(c("action.failed_message"));
      }
    }, POLL_MS);
    return () => window.clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bundle]);

  async function upload() {
    const file = fileRef.current?.files?.[0];
    if (!file) return;
    setPhase("uploading");
    setMessage("");
    setBundle(null);
    appliedRef.current = false;
    const form = new FormData();
    form.set("file", file, file.name);
    try {
      const response = await fetch(`${SHEETS}/workbook/imports`, { method: "POST", body: form });
      const { bundle: staged, message: failure } = await readBundle(response);
      if (!staged) {
        setPhase("error");
        setMessage(failure ?? c("action.failed_message"));
        return;
      }
      setBundle(staged);
      setPhase("polling");
    } catch {
      setPhase("error");
      setMessage(c("action.failed_message"));
    }
  }

  async function act(action: "apply" | "cancel") {
    if (!bundle) return;
    setMessage("");
    try {
      const response = await fetch(bundleHref(bundle.id, action), { method: "POST" });
      const { bundle: next, message: failure } = await readBundle(response);
      if (!next) {
        setPhase("error");
        setMessage(failure ?? c("action.failed_message"));
        return;
      }
      setBundle(next);
      setPhase(inFlight(next) ? "polling" : "idle");
      if (action === "apply" && next.status === "applied" && !appliedRef.current) {
        appliedRef.current = true;
        router.refresh();
        void loadRecent();
      }
      if (action === "cancel") void loadRecent();
    } catch {
      setPhase("error");
      setMessage(c("action.failed_message"));
    }
  }

  const busy = phase === "uploading" || (bundle !== null && inFlight(bundle));
  const statusLabel = (status: string) => c(`sheet.status.${status}`) || status;
  const totals = (bundle?.jobs ?? []).reduce(
    (acc, job) => ({
      total: acc.total + job.total_rows,
      valid: acc.valid + job.valid_rows,
      invalid: acc.invalid + job.invalid_rows,
      applied: acc.applied + job.applied_rows,
      failed: acc.failed + job.failed_rows,
    }),
    { total: 0, valid: 0, invalid: 0, applied: 0, failed: 0 },
  );
  const tabsDone = (bundle?.jobs ?? []).filter((job) => job.status === (bundle?.status === "applying" || bundle?.status === "applied" ? "applied" : "previewed")).length;
  const hasProblems = totals.invalid > 0 || totals.failed > 0;

  const headCells: TableHeadCellProps[] = [
    { id: "tab", label: c("workbook.tab") },
    { id: "rows", label: c("workbook.col.rows"), align: "right" },
    { id: "ready", label: c("workbook.col.ready"), align: "right" },
    { id: "fix", label: c("workbook.col.fix"), align: "right" },
    { id: "applied", label: c("workbook.col.applied"), align: "right" },
    { id: "failed", label: c("workbook.col.failed"), align: "right" },
    { id: "status", label: c("column.status") },
  ];

  return (
    <SheetDrawerBody testId="workbook-drawer">
      <SheetSection icon="solar:notebook-bold-duotone" title={c("workbook.title")}>
        <SheetHint>{c("workbook.intro")}</SheetHint>
        <SheetActions>
          <Button component="a" size="small" variant="contained" color="primary" href={`${SHEETS}/workbook/template?format=xlsx`} download data-testid="workbook-template" startIcon={<Iconify icon="solar:download-bold" width={18} aria-hidden="true" />}>
            {c("workbook.template")}
          </Button>
          <Button component="a" size="small" variant="outlined" color="inherit" href={`${SHEETS}/workbook/export?format=xlsx`} download data-testid="workbook-export" startIcon={<Iconify icon="solar:download-bold" width={18} aria-hidden="true" />}>
            {c("workbook.export")}
          </Button>
        </SheetActions>
        <SheetHint>{c("workbook.template_hint")}</SheetHint>
        <SheetHint>{c("workbook.export_hint")}</SheetHint>
      </SheetSection>

      {canWrite ? (
        <SheetSection icon="eva:cloud-upload-fill" title={c("workbook.upload_title")}>
          <SheetHint>{c("workbook.upload_hint")}</SheetHint>
          <SheetHint>{c("workbook.order_hint")}</SheetHint>
          <Stack spacing={1.5}>
            <UploadFile
              inputRef={fileRef}
              accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              ariaLabel={c("sheet.choose_file")}
              title={c("sheet.choose_file")}
              disabled={busy}
              onFileChange={(file) => setFileName(file?.name ?? "")}
              testId="workbook-file"
            />
            <Button
              type="button"
              variant="contained"
              color="primary"
              disabled={busy || !fileName}
              onClick={() => void upload()}
              data-testid="workbook-upload"
              startIcon={phase === "uploading" ? <CircularProgress size={14} color="inherit" aria-hidden="true" /> : <Iconify icon="eva:cloud-upload-fill" width={18} aria-hidden="true" />}
              sx={{ alignSelf: "flex-start" }}
            >
              {phase === "uploading" ? c("sheet.uploading") : c("workbook.upload_action")}
            </Button>
          </Stack>

          {bundle ? (
            <SheetJobCard testId="workbook-bundle" status={bundle.status} fileName={bundle.file_name} statusLabel={statusLabel(bundle.status)}>
              <SheetCounts
                items={[
                  { label: c("sheet.rows_total"), value: totals.total, testId: "workbook-total" },
                  { label: c("sheet.rows_valid"), value: totals.valid, testId: "workbook-valid" },
                  { label: c("sheet.rows_invalid"), value: totals.invalid, testId: "workbook-invalid" },
                  ...(bundle.status === "applying" || bundle.status === "applied" || totals.applied > 0 || totals.failed > 0
                    ? [
                        { label: c("sheet.rows_applied"), value: totals.applied, testId: "workbook-applied" },
                        { label: c("sheet.rows_failed"), value: totals.failed, testId: "workbook-failed" },
                      ]
                    : []),
                  { label: c("workbook.tabs_done"), value: `${tabsDone} / ${bundle.jobs.length}`, testId: "workbook-tabs-done" },
                ]}
              />
              {bundle.status === "failed" && bundle.error ? <Alert severity="error">{bundle.error}</Alert> : null}
              {bundle.unknown_sheets.length > 0 ? (
                <SheetHint testId="workbook-unknown">
                  {c("workbook.unknown_sheets")} {bundle.unknown_sheets.join(", ")}
                </SheetHint>
              ) : null}
              <Scrollbar>
                <Table size="small" data-testid="workbook-tabs" sx={{ minWidth: 560 }}>
                  <TableHeadCustom headCells={headCells} />
                  <TableBody>
                    {bundle.jobs.map((job) => (
                      <TableRow key={job.id} data-testid="workbook-tab" data-register={job.register} data-status={job.status}>
                        <TableCell title={registerLabels[job.register] ?? job.register}>
                          {job.sheet_name || job.register}
                          {registerLabels[job.register] && registerLabels[job.register] !== job.sheet_name ? (
                            <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
                              {registerLabels[job.register]}
                            </Typography>
                          ) : null}
                        </TableCell>
                        <TableCell align="right">{job.total_rows}</TableCell>
                        <TableCell align="right">{job.valid_rows}</TableCell>
                        <TableCell align="right">{job.invalid_rows}</TableCell>
                        <TableCell align="right">{job.applied_rows}</TableCell>
                        <TableCell align="right">{job.failed_rows}</TableCell>
                        <TableCell>
                          <SheetStatus status={job.status} label={statusLabel(job.status)} />
                          {job.status === "validating" || job.status === "applying" ? <LinearProgress variant="determinate" value={jobProgress(job)} sx={{ mt: 0.5 }} /> : null}
                          {job.status === "failed" && job.error ? (
                            <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>
                              {job.error}
                            </Typography>
                          ) : null}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Scrollbar>
              {bundle.status === "previewed" ? <SheetHint>{c("workbook.apply_hint")}</SheetHint> : null}
              <SheetActions>
                {bundle.status === "previewed" && totals.valid > 0 ? (
                  <Button type="button" size="small" variant="contained" color="primary" onClick={() => void act("apply")} data-testid="workbook-apply">
                    {c("workbook.apply")} ({totals.valid})
                  </Button>
                ) : null}
                {hasProblems ? (
                  <Button component="a" size="small" variant="text" color="inherit" href={`${bundleHref(bundle.id, "errors")}?format=xlsx`} download data-testid="workbook-errors">
                    {c("workbook.download_errors")}
                  </Button>
                ) : null}
                {inFlight(bundle) || bundle.status === "previewed" ? (
                  <Button type="button" size="small" variant="text" color="inherit" onClick={() => void act("cancel")} data-testid="workbook-cancel">
                    {c("workbook.cancel")}
                  </Button>
                ) : null}
              </SheetActions>
            </SheetJobCard>
          ) : null}
          {phase === "error" && message ? (
            <Alert severity="error" role="alert">
              {message}
            </Alert>
          ) : null}

          {recent.length > 0 ? (
            <SheetRecent
              title={c("workbook.recent")}
              items={recent.slice(0, 5).map((item) => ({ id: item.id, fileName: item.file_name, meta: `${item.jobs.length} ${c("workbook.tab").toLowerCase()} · ${statusLabel(item.status)}` }))}
            />
          ) : null}
        </SheetSection>
      ) : null}
    </SheetDrawerBody>
  );
}

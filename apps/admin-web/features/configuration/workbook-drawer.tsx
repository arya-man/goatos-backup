"use client";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";

import CircularProgress from "@mui/material/CircularProgress";
import LinearProgress from "@mui/material/LinearProgress";
import { BookOpen, Download, Upload } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ConfigurationImportBundle, ConfigurationImportJob } from "@/lib/api/configuration-server";
import { UploadFile } from "@/components/app/upload-file";
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";

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

  return (
    <div className="cfg-sheet" data-testid="workbook-drawer">
      <section className="cfg-sheet-section">
        <div className="cfg-sheet-title">
          <BookOpen className="ic" aria-hidden="true" /> {c("workbook.title")}
        </div>
        <p className="muted small">{c("workbook.intro")}</p>
        <div className="cfg-sheet-actions">
          <a className="btn sm b" href={`${SHEETS}/workbook/template?format=xlsx`} download data-testid="workbook-template">
            <Download className="ic" aria-hidden="true" /> {c("workbook.template")}
          </a>
          <a className="btn sm" href={`${SHEETS}/workbook/export?format=xlsx`} download data-testid="workbook-export">
            <Download className="ic" aria-hidden="true" /> {c("workbook.export")}
          </a>
        </div>
        <p className="muted small">{c("workbook.template_hint")}</p>
        <p className="muted small">{c("workbook.export_hint")}</p>
      </section>

      {canWrite ? (
        <section className="cfg-sheet-section">
          <div className="cfg-sheet-title">
            <Upload className="ic" aria-hidden="true" /> {c("workbook.upload_title")}
          </div>
          <p className="muted small">{c("workbook.upload_hint")}</p>
          <p className="muted small">{c("workbook.order_hint")}</p>
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
              startIcon={phase === "uploading" ? <CircularProgress size={14} color="inherit" aria-hidden="true" /> : <Upload className="ic" aria-hidden="true" />}
              sx={{ alignSelf: "flex-start" }}
            >
              {phase === "uploading" ? c("sheet.uploading") : c("workbook.upload_action")}
            </Button>
          </Stack>

          {bundle ? (
            <div className="cfg-sheet-job" data-testid="workbook-bundle" data-status={bundle.status}>
              <div className="cfg-sheet-job-head">
                <b className="cfg-sheet-file-name">{bundle.file_name}</b>
                <span className={`tag ${bundle.status === "applied" ? "ok" : bundle.status === "failed" ? "bad" : ""}`}>{statusLabel(bundle.status)}</span>
              </div>
              <dl className="cfg-sheet-counts">
                <div>
                  <dt>{c("sheet.rows_total")}</dt>
                  <dd data-testid="workbook-total">{totals.total}</dd>
                </div>
                <div>
                  <dt>{c("sheet.rows_valid")}</dt>
                  <dd data-testid="workbook-valid">{totals.valid}</dd>
                </div>
                <div>
                  <dt>{c("sheet.rows_invalid")}</dt>
                  <dd data-testid="workbook-invalid">{totals.invalid}</dd>
                </div>
                {bundle.status === "applying" || bundle.status === "applied" || totals.applied > 0 || totals.failed > 0 ? (
                  <>
                    <div>
                      <dt>{c("sheet.rows_applied")}</dt>
                      <dd data-testid="workbook-applied">{totals.applied}</dd>
                    </div>
                    <div>
                      <dt>{c("sheet.rows_failed")}</dt>
                      <dd data-testid="workbook-failed">{totals.failed}</dd>
                    </div>
                  </>
                ) : null}
                <div>
                  <dt>{c("workbook.tabs_done")}</dt>
                  <dd data-testid="workbook-tabs-done">
                    {tabsDone} / {bundle.jobs.length}
                  </dd>
                </div>
              </dl>
              {bundle.status === "failed" && bundle.error ? <div className="note bad">{bundle.error}</div> : null}
              {bundle.unknown_sheets.length > 0 ? (
                <p className="muted small" data-testid="workbook-unknown">
                  {c("workbook.unknown_sheets")} {bundle.unknown_sheets.join(", ")}
                </p>
              ) : null}
              <div className="cfg-workbook-scroll">
              <Table className="tbl cfg-workbook-tabs" data-testid="workbook-tabs">
                <TableHead>
                  <TableRow>
                    <TableCell component="th">{c("workbook.tab")}</TableCell>
                    <TableCell component="th" className="num">{c("workbook.col.rows")}</TableCell>
                    <TableCell component="th" className="num">{c("workbook.col.ready")}</TableCell>
                    <TableCell component="th" className="num">{c("workbook.col.fix")}</TableCell>
                    <TableCell component="th" className="num">{c("workbook.col.applied")}</TableCell>
                    <TableCell component="th" className="num">{c("workbook.col.failed")}</TableCell>
                    <TableCell component="th">{c("column.status")}</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {bundle.jobs.map((job) => (
                    <TableRow key={job.id} data-testid="workbook-tab" data-register={job.register} data-status={job.status}>
                      <TableCell title={registerLabels[job.register] ?? job.register}>
                        {job.sheet_name || job.register}
                        {registerLabels[job.register] && registerLabels[job.register] !== job.sheet_name ? <div className="muted small">{registerLabels[job.register]}</div> : null}
                      </TableCell>
                      <TableCell className="num">{job.total_rows}</TableCell>
                      <TableCell className="num">{job.valid_rows}</TableCell>
                      <TableCell className="num">{job.invalid_rows}</TableCell>
                      <TableCell className="num">{job.applied_rows}</TableCell>
                      <TableCell className="num">{job.failed_rows}</TableCell>
                      <TableCell>
                        <span className={`tag ${job.status === "applied" ? "ok" : job.status === "failed" ? "bad" : ""}`}>{statusLabel(job.status)}</span>
                        {job.status === "validating" || job.status === "applying" ? (
                          <LinearProgress variant="determinate" value={jobProgress(job)} />
                        ) : null}
                        {job.status === "failed" && job.error ? <div className="muted small">{job.error}</div> : null}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              </div>
              {bundle.status === "previewed" ? <p className="muted small">{c("workbook.apply_hint")}</p> : null}
              <div className="cfg-sheet-actions">
                {bundle.status === "previewed" && totals.valid > 0 ? (
                  <button type="button" className="btn sm b" onClick={() => void act("apply")} data-testid="workbook-apply">
                    {c("workbook.apply")} ({totals.valid})
                  </button>
                ) : null}
                {hasProblems ? (
                  <a className="btn sm ghost" href={`${bundleHref(bundle.id, "errors")}?format=xlsx`} download data-testid="workbook-errors">
                    {c("workbook.download_errors")}
                  </a>
                ) : null}
                {inFlight(bundle) || bundle.status === "previewed" ? (
                  <button type="button" className="btn sm ghost" onClick={() => void act("cancel")} data-testid="workbook-cancel">
                    {c("workbook.cancel")}
                  </button>
                ) : null}
              </div>
            </div>
          ) : null}
          {phase === "error" && message ? (
            <div className="note bad" role="alert">
              {message}
            </div>
          ) : null}

          {recent.length > 0 ? (
            <div className="cfg-sheet-recent">
              <div className="cfg-sheet-title small">{c("workbook.recent")}</div>
              <ul>
                {recent.slice(0, 5).map((item) => (
                  <li key={item.id}>
                    <span className="cfg-sheet-file-name">{item.file_name}</span>
                    <span className="muted small cfg-sheet-recent-meta">
                      {item.jobs.length} {c("workbook.tab").toLowerCase()} · {statusLabel(item.status)}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}

"use client";

import CircularProgress from "@mui/material/CircularProgress";
import LinearProgress from "@mui/material/LinearProgress";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ConfigurationImportJob, ConfigurationImportRow, ConfigurationRegister } from "@/lib/api/configuration-server";
import { UploadFile } from "@/components/app/upload-file";
import Button from "@mui/material/Button";
import Stack from "@mui/material/Stack";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { SheetActions, SheetCounts, SheetDrawerBody, SheetHint, SheetJobCard, SheetRecent, SheetSection, SheetSubtitle } from "./sheet-drawer-parts";

/**
 * The bulk sheet drawer (maintainer instruction 2026-09-18): DOWNLOAD a register as CSV or
 * Excel, and UPLOAD one back -- preview, then apply. Files move through the same-origin proxy
 * at /api/admin/configuration-sheets, which streams both ways; this component holds a job id
 * and its counts, never a sheet.
 *
 * The upload is a JOB the backend works in the background: the drawer posts the file, then
 * polls the job while it is checking or applying and shows the backend-owned counts as they
 * move. Every sentence is page-contract copy; the register's own name comes from the register.
 */

const SHEETS = "/api/admin/configuration-sheets";
const POLL_MS = 1500;
const PROBLEM_ROWS = 8;

type Phase = "idle" | "uploading" | "polling" | "error";

function sheetHref(register: string, action: string, format: "csv" | "xlsx"): string {
  return `${SHEETS}/${encodeURIComponent(register)}/${action}?format=${format}`;
}

function jobHref(jobId: string, action = ""): string {
  return `${SHEETS}/jobs/${encodeURIComponent(jobId)}${action ? `/${action}` : ""}`;
}

async function readJob(response: Response): Promise<{ job?: ConfigurationImportJob; message?: string }> {
  let body: unknown = null;
  try {
    body = await response.json();
  } catch {
    body = null;
  }
  const record = (body ?? {}) as { job?: ConfigurationImportJob; message?: string; error?: string };
  if (!response.ok) return { message: record.message ?? record.error ?? `HTTP ${response.status}` };
  return { job: record.job };
}

function inFlight(job: ConfigurationImportJob | null): boolean {
  return job?.status === "validating" || job?.status === "applying";
}

export function SheetDrawer({
  pageContract,
  register,
  canWrite,
}: {
  pageContract: AdminUiPageContract;
  register: ConfigurationRegister;
  canWrite: boolean;
}) {
  const c = (key: string) => copy(pageContract, key);
  const router = useRouter();
  const [phase, setPhase] = useState<Phase>("idle");
  const [message, setMessage] = useState("");
  const [job, setJob] = useState<ConfigurationImportJob | null>(null);
  const [problems, setProblems] = useState<ConfigurationImportRow[]>([]);
  const [recent, setRecent] = useState<ConfigurationImportJob[]>([]);
  const [fileName, setFileName] = useState("");
  const fileRef = useRef<HTMLInputElement>(null);
  const appliedRef = useRef(false);

  const loadRecent = useCallback(async () => {
    try {
      const response = await fetch(`${SHEETS}/${encodeURIComponent(register.key)}/imports`, { cache: "no-store" });
      if (!response.ok) return;
      const body = (await response.json()) as { jobs?: ConfigurationImportJob[] };
      setRecent(body.jobs ?? []);
    } catch {
      // The recent list is a convenience; the drawer works without it.
    }
  }, [register.key]);

  // The recent list is fetched once the drawer has painted (a scheduled fetch, not a
  // synchronous setState inside the effect).
  useEffect(() => {
    if (!register.importable || !canWrite) return;
    const timer = window.setTimeout(() => void loadRecent(), 0);
    return () => window.clearTimeout(timer);
  }, [register.importable, canWrite, loadRecent]);

  const loadProblems = useCallback(async (jobId: string) => {
    try {
      const response = await fetch(`${jobHref(jobId, "rows")}?state=invalid&limit=${PROBLEM_ROWS}`, { cache: "no-store" });
      if (!response.ok) return;
      const body = (await response.json()) as { rows?: ConfigurationImportRow[] };
      setProblems(body.rows ?? []);
    } catch {
      setProblems([]);
    }
  }, []);

  // Poll while the backend is checking or applying.
  useEffect(() => {
    if (!job || !inFlight(job)) return;
    const timer = window.setTimeout(async () => {
      try {
        const response = await fetch(jobHref(job.id), { cache: "no-store" });
        const { job: next, message: failure } = await readJob(response);
        if (!next) {
          setPhase("error");
          setMessage(failure ?? c("action.failed_message"));
          return;
        }
        setJob(next);
        if (next.status === "previewed" && next.invalid_rows > 0) void loadProblems(next.id);
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
  }, [job]);

  async function upload() {
    const file = fileRef.current?.files?.[0];
    if (!file) return;
    setPhase("uploading");
    setMessage("");
    setProblems([]);
    setJob(null);
    appliedRef.current = false;
    const form = new FormData();
    form.set("file", file, file.name);
    try {
      const response = await fetch(`${SHEETS}/${encodeURIComponent(register.key)}/imports`, { method: "POST", body: form });
      const { job: staged, message: failure } = await readJob(response);
      if (!staged) {
        setPhase("error");
        setMessage(failure ?? c("action.failed_message"));
        return;
      }
      setJob(staged);
      setPhase("polling");
    } catch {
      setPhase("error");
      setMessage(c("action.failed_message"));
    }
  }

  async function act(action: "apply" | "cancel") {
    if (!job) return;
    setMessage("");
    try {
      const response = await fetch(jobHref(job.id, action), { method: "POST" });
      const { job: next, message: failure } = await readJob(response);
      if (!next) {
        setPhase("error");
        setMessage(failure ?? c("action.failed_message"));
        return;
      }
      setJob(next);
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

  const busy = phase === "uploading" || (job !== null && inFlight(job));
  const progress = job && job.total_rows > 0 ? Math.min(100, Math.round((job.progress_row_no / (job.total_rows + 1)) * 100)) : 0;
  const statusLabel = (status: string) => c(`sheet.status.${status}`) || status;

  return (
    <SheetDrawerBody testId="sheet-drawer">
      <SheetSection icon="solar:download-bold" title={c("sheet.download")}>
        <SheetHint>{register.import_create_only ? c("sheet.download_hint_animals") : c("sheet.download_hint")}</SheetHint>
        <SheetActions>
          <Button component="a" size="small" variant="outlined" color="inherit" href={sheetHref(register.key, "export", "csv")} download data-testid="sheet-download-csv" startIcon={<Iconify icon="solar:file-text-bold" width={18} aria-hidden="true" />}>
            {c("sheet.download_csv")}
          </Button>
          <Button component="a" size="small" variant="outlined" color="inherit" href={sheetHref(register.key, "export", "xlsx")} download data-testid="sheet-download-xlsx" startIcon={<Iconify icon="solar:file-text-bold" width={18} aria-hidden="true" />}>
            {c("sheet.download_xlsx")}
          </Button>
        </SheetActions>
      </SheetSection>

      {register.importable && canWrite ? (
        <SheetSection icon="eva:cloud-upload-fill" title={c("sheet.upload_title")}>
          <SheetHint>{register.key === "animals" ? c("sheet.upload_hint_animals") : c("sheet.upload_hint")}</SheetHint>
          <SheetActions>
            <Button component="a" size="small" variant="text" color="inherit" href={sheetHref(register.key, "template", "csv")} download data-testid="sheet-template-csv">
              {c("sheet.template")} (CSV)
            </Button>
            <Button component="a" size="small" variant="text" color="inherit" href={sheetHref(register.key, "template", "xlsx")} download data-testid="sheet-template-xlsx">
              {c("sheet.template")} (Excel)
            </Button>
          </SheetActions>
          <SheetHint>{c("sheet.template_hint")}</SheetHint>
          <Stack spacing={1.5}>
            <UploadFile
              inputRef={fileRef}
              accept=".csv,.xlsx,text/csv,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
              ariaLabel={c("sheet.choose_file")}
              title={c("sheet.choose_file")}
              disabled={busy}
              onFileChange={(file) => setFileName(file?.name ?? "")}
              testId="sheet-file"
            />
            <Button
              type="button"
              variant="contained"
              color="primary"
              disabled={busy || !fileName}
              onClick={() => void upload()}
              data-testid="sheet-upload"
              startIcon={phase === "uploading" ? <CircularProgress size={14} color="inherit" aria-hidden="true" /> : <Iconify icon="eva:cloud-upload-fill" width={18} aria-hidden="true" />}
              sx={{ alignSelf: "flex-start" }}
            >
              {phase === "uploading" ? c("sheet.uploading") : c("sheet.upload_action")}
            </Button>
          </Stack>

          {job ? (
            <SheetJobCard testId="sheet-job" status={job.status} fileName={job.file_name} statusLabel={statusLabel(job.status)}>
              {inFlight(job) ? <LinearProgress variant="determinate" value={progress} /> : null}
              <SheetCounts
                items={[
                  { label: c("sheet.rows_total"), value: job.total_rows, testId: "sheet-total" },
                  { label: c("sheet.rows_valid"), value: job.valid_rows, testId: "sheet-valid" },
                  { label: c("sheet.rows_invalid"), value: job.invalid_rows, testId: "sheet-invalid" },
                  ...(job.status === "applying" || job.status === "applied" || job.applied_rows > 0 || job.failed_rows > 0
                    ? [
                        { label: c("sheet.rows_applied"), value: job.applied_rows, testId: "sheet-applied" },
                        { label: c("sheet.rows_failed"), value: job.failed_rows, testId: "sheet-failed" },
                      ]
                    : []),
                ]}
              />
              {job.status === "failed" && job.error ? <Alert severity="error">{job.error}</Alert> : null}
              {job.status === "previewed" ? <SheetHint>{c("sheet.apply_hint")}</SheetHint> : null}
              {problems.length > 0 && (job.status === "previewed" || job.status === "cancelled") ? (
                <Stack spacing={0.5}>
                  <SheetSubtitle>{c("sheet.problems_title")}</SheetSubtitle>
                  <Box component="ul" sx={{ m: 0, pl: 2, display: "flex", flexDirection: "column", gap: 0.5 }}>
                    {problems.map((row) => (
                      <Typography component="li" variant="body2" key={row.row_no}>
                        <Box component="b" sx={{ fontWeight: "fontWeightSemiBold" }}>
                          {c("sheet.problem_row")} {row.row_no}
                        </Box>
                        : {row.errors.map((error) => (error.field && error.field !== "row" ? `${error.field}: ${error.message}` : error.message)).join(" · ")}
                      </Typography>
                    ))}
                  </Box>
                </Stack>
              ) : null}
              <SheetActions>
                {job.status === "previewed" && job.valid_rows > 0 ? (
                  <Button type="button" size="small" variant="contained" color="primary" onClick={() => void act("apply")} data-testid="sheet-apply">
                    {c("sheet.apply")} ({job.valid_rows})
                  </Button>
                ) : null}
                {job.invalid_rows > 0 || job.failed_rows > 0 ? (
                  <Button component="a" size="small" variant="text" color="inherit" href={`${jobHref(job.id, "errors")}?format=csv`} download data-testid="sheet-errors">
                    {c("sheet.download_errors")}
                  </Button>
                ) : null}
                {inFlight(job) || job.status === "previewed" ? (
                  <Button type="button" size="small" variant="text" color="inherit" onClick={() => void act("cancel")} data-testid="sheet-cancel">
                    {c("sheet.cancel")}
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
              title={c("sheet.recent")}
              items={recent.slice(0, 5).map((item) => ({ id: item.id, fileName: item.file_name, meta: `${item.total_rows} · ${statusLabel(item.status)}` }))}
            />
          ) : null}
        </SheetSection>
      ) : null}
    </SheetDrawerBody>
  );
}

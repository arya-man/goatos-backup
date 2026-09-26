"use client";

import { Tag } from "@/components/ui-primitives";

import { useCallback, useMemo, useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import {
  BookText,
  Calendar,
  Camera,
  Check,
  Columns3,
  Download,
  FileText,
  Info,
  Clock,
  Hash,
  List,
  ListChecks,
  MapPin,
  NotebookPen,
  Plus,
  ScanLine,
  SquarePen,
  Type as TypeIcon,
  Users,
  Video,
  X,
  Zap,
} from "lucide-react";
import { type SopCardView, type SopTrigger } from "./sop-derive";
import { FollowUpStepsSummary } from "./followup-summary";
import { InspectionSummary } from "./inspection-summary";
import { PcCareSummary } from "./pc-care-summary";
import { WeighingSummary } from "./weighing-summary";
import { FeedSummary } from "./feed-summary";
import Card from "@mui/material/Card";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import IconButton from "@mui/material/IconButton";
import { Iconify } from "@/components/minimal/iconify";
import { RowMenu } from "@/components/app/row-menu";
import { FilterBar } from "@/components/app/filter-bar";
import { TableFooter } from "@/components/app/table-footer";
import { DenseToggle } from "@/components/app/dense-toggle";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { ShiftingSummary } from "./shifting-summary";
import { CaptureCardSummary } from "./capture-summary";
import { isCaptureCardCode } from "./capture-model";
import { copy, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { PageHeader } from "@/components/app/page-header";
import Switch from "@mui/material/Switch";
import FormControlLabel from "@mui/material/FormControlLabel";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import type { Theme } from "@mui/material/styles";
import Chip from "@mui/material/Chip";
import MuiCard from "@mui/material/Card";
import Paper from "@mui/material/Paper";
import { StatStrip } from "@/components/minimal/widgets/stat-strip";

// The New SOP builder is a dedicated full-page surface at <module SOP page>?compose=1 — the same
// route as the module page (never a nested /new page). Legacy `?new=1` deep-links resolve to it too.

const TRIGGER_ICON: Record<SopTrigger, React.ElementType> = {
  form: SquarePen,
  cron: Clock,
  sensor: Zap,
  manual: Users,
};

const FIELD_ICON: Record<string, React.ElementType> = {
  text: TypeIcon,
  number: Hash,
  date_time: Calendar,
  select: List,
  multiselect: ListChecks,
  goat_lookup: ScanLine,
  animal_id_scan: ScanLine,
  location_picker: MapPin,
  photo_proof: Camera,
  video_proof: Video,
};

type FacetId = "domain" | "trigger" | "counts" | "gates";

/** Title-cases the module segment of e.g. "/counts/sops" for the breadcrumb trail. */
function moduleSegment(basePath: string): string {
  const seg = basePath.split("/").filter(Boolean)[0] ?? "";
  return seg ? seg.charAt(0).toUpperCase() + seg.slice(1) : "";
}

const STATUS_TONE: Record<SopCardView["status"], string> = { active: "t-ok", draft: "t-mut", retired: "t-warn" };

// Card footer (spec §9) and dialog body rhythm: theme spacing/typography only.
const CARD_FOOT_SX = { display: "flex", alignItems: "center", gap: 1, mt: 1.25, pt: 1.25, borderTop: 1, borderTopStyle: "dashed", borderColor: "divider" } as const;
const CARD_META_SX = { minWidth: 0, typography: "caption", color: "text.secondary", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" } as const;
const DLG_BODY_SX = {
  "& .htl > .hrow": { borderRadius: "var(--r-md)", transition: (t: Theme) => t.transitions.create("background-color") },
  "& .htl > .hrow:hover": { bgcolor: "action.hover" },
} as const;
function StatusTag({ view }: { view: SopCardView }) {
  const label = view.status === "active" ? `published${view.versionNumber ? ` · v${view.versionNumber}` : ""}` : view.status;
  return <span className={`tag ${STATUS_TONE[view.status]}`}>{label}</span>;
}

export interface SopLibraryProps {
  sops: SopCardView[];
  error?: { code?: string; message: string } | null;
  authRequired?: boolean;
  pageContract: AdminUiPageContract;
  /** The module SOP page path this library is mounted on (e.g. "/vaccination/sops"). */
  basePath: string;
  /** Set when an editor just published: the banner names the version and that card is lit. */
  published?: { sopId: string; version: number | null } | null;
  /** Extra header controls a module page mounts beside "New SOP" (Weighing: the Assumptions drawer). */
  extraNode?: React.ReactNode;
}

// SOP Library client console. Ported from the mock SOP Library screen (header, search, domain chips,
// card grid, detail modal). "New SOP" / "Edit" navigate to the dedicated full-page builder
// (<basePath>?compose=1 [&edit=<sop_id>]). Cards render ONLY real `/admin/sops` data; facets are derived from
// real code/description/form_dsl/proof_policy. No mock inventory, no fake source rows.
export function SopLibrary({ sops, error, authRequired, pageContract, basePath, published, extraNode }: SopLibraryProps) {
  const router = useRouter();
  const builderHref = `${basePath}?compose=1`;
  // The just-published banner is dismissed locally (no navigation) and is only shown while the
  // named SOP is actually on this page.
  const [publishedDismissed, setPublishedDismissed] = useState(false);
  const publishedSop = published && !publishedDismissed ? sops.find((s) => s.sopId === published.sopId) ?? null : null;
  const publishedVersion = published?.version ?? publishedSop?.versionNumber ?? null;
  const publishedTitle = publishedSop
    ? copy(pageContract, "notice.published.title")
        .replace("{version}", publishedVersion === null ? "" : String(publishedVersion))
        .replace("v — ", "— ")
        .replace("{name}", publishedSop.name)
    : "";
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [triggerFilter, setTriggerFilter] = useState("");
  const [dense, setDense] = useState(false);
  const [columnsOpen, setColumnsOpen] = useState(false);
  const [facets, setFacets] = useState<Record<FacetId, boolean>>({ domain: true, trigger: true, counts: true, gates: true });
  const [detail, setDetail] = useState<SopCardView | null>(null);
  const openBuilder = () => router.push(builderHref);
  // The editor is a server-rendered route; keep the drawer open (button shows "Opening…") until
  // the navigation commits, otherwise a slow first compile looks like "the modal closed and
  // nothing opened".
  const [editorPending, startEditorNav] = useTransition();
  const openEditor = (sopId: string) => startEditorNav(() => router.push(`${builderHref}&edit=${sopId}`));
  const openCaptureEditor = (sopId: string) => startEditorNav(() => router.push(`${builderHref}&edit=${sopId}&part=capture`));
  const [requestedPage, setRequestedPage] = useState(1);
	  const pageSizeOptions = tablePageSizes(pageContract, "sop-library");
	  const [pageSize, setPageSize] = useState<number>(pageSizeOptions.includes(10) ? 10 : (pageSizeOptions[0] ?? 10));
  // The page is pre-scoped to its module's SOP codes (SOP split, maintainer decision 2026-08-18).
  // Status and trigger are derived from the cards already in hand — no extra backend call.
  const list = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sops.filter((s) => {
      if (q && !`${s.name} ${s.code} ${s.domainLabel}`.toLowerCase().includes(q)) return false;
      if (statusFilter && s.status !== statusFilter) return false;
      if (triggerFilter && (s.trigger ?? "") !== triggerFilter) return false;
      return true;
    });
  }, [sops, query, statusFilter, triggerFilter]);
  const totalPages = Math.max(1, Math.ceil(list.length / pageSize));
  const page = Math.min(requestedPage, totalPages);
  const pagedList = list.slice((page - 1) * pageSize, page * pageSize);

  const stats = useMemo(
    () => ({
      total: sops.length,
      active: sops.filter((s) => s.status === "active").length,
      draft: sops.filter((s) => s.status === "draft").length,
      retired: sops.filter((s) => s.status === "retired").length,
    }),
    [sops],
  );

  const statusOptions = useMemo(
    () => [
      { value: "", label: copy(pageContract, "filter.status.all", "All statuses") },
      { value: "active", label: copy(pageContract, "filter.status.active") },
      { value: "draft", label: copy(pageContract, "filter.status.draft") },
      { value: "retired", label: copy(pageContract, "filter.status.retired") },
    ],
    [pageContract],
  );
  const triggerOptions = useMemo(
    () => [
      { value: "", label: copy(pageContract, "filter.trigger.all") },
      ...(["form", "cron", "sensor", "manual"] as const).map((t) => ({ value: t, label: t })),
    ],
    [pageContract],
  );

  const activeChips: Array<{ id: string; label: string; clear: () => void }> = [];
  if (query.trim()) activeChips.push({ id: "q", label: `"${query.trim()}"`, clear: () => setQuery("") });
  if (statusFilter)
    activeChips.push({
      id: "status",
      label: statusOptions.find((o) => o.value === statusFilter)?.label ?? statusFilter,
      clear: () => setStatusFilter(""),
    });
  if (triggerFilter) activeChips.push({ id: "trigger", label: triggerFilter, clear: () => setTriggerFilter("") });
  const clearAll = useCallback(() => {
    setQuery("");
    setStatusFilter("");
    setTriggerFilter("");
    setRequestedPage(1);
  }, []);

  // Export is read-only: it serialises exactly the rows already on screen, client-side.
  const exportCsv = useCallback(() => {
    const head = ["code", "name", "domain", "trigger", "status", "version", "steps", "questions"];
    const rows = list.map((s) => [
      s.code,
      s.name,
      s.domainLabel,
      s.trigger ?? "",
      s.status,
      s.versionLabel ?? "",
      s.stepCount ?? "",
      s.inspectionQuestionCount,
    ]);
    const csv = [head, ...rows]
      .map((r) => r.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(","))
      .join("\r\n");
    const url = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `sops-${basePath.replace(/\W+/g, "-").replace(/^-|-$/g, "")}.csv`;
    a.click();
    URL.revokeObjectURL(url);
  }, [list, basePath]);

  return (
    <div className="kit-enter screen on sop-kit">
      <div>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb", moduleSegment(basePath)) || moduleSegment(basePath) }, { label: pageContract.title }]}
          actions={
            <Box sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 1, flexWrap: "wrap", "& .wt-assumptions-control": { display: "contents" } }}>
              {extraNode}
              <Button variant="contained" color="primary" startIcon={<Plus size={18} />} onClick={openBuilder}>
                {copy(pageContract, "action.new_sop")}
              </Button>
            </Box>
          }
        />
      </div>

      {/* Spec §5 stat strip — derived from the cards already in hand. Template invoice list:
          the InvoiceAnalytic row inside its own Card. */}
      <MuiCard>
        <StatStrip
          cells={[
            { key: "total", icon: <BookText aria-hidden="true" />, label: copy(pageContract, "stat.total"), value: stats.total, tone: "primary" },
            { key: "active", icon: <Check aria-hidden="true" />, label: copy(pageContract, "filter.status.active"), value: stats.active, tone: "success" },
            { key: "draft", icon: <FileText aria-hidden="true" />, label: copy(pageContract, "filter.status.draft"), value: stats.draft, tone: "warning" },
            { key: "retired", icon: <Info aria-hidden="true" />, label: copy(pageContract, "filter.status.retired"), value: stats.retired, tone: "neutral" },
          ]}
        />
      </MuiCard>

      {/* Spec §2 toolbar: search + status/trigger filters, right-aligned actions, filter chips. */}
      <div>
        <FilterBar
          fold={{ label: copy(pageContract, "action.filters", "Filters"), count: activeChips.filter((chip) => chip.id !== "q").length }}
          search={{
            value: query,
            placeholder: copy(pageContract, "filter.search_placeholder"),
            ariaLabel: copy(pageContract, "filter.search_label"),
            onChange: (v) => {
              setQuery(v);
              setRequestedPage(1);
            },
          }}
          actions={
            <>
              <Button color="primary" variant="text" size="small" startIcon={<Columns3 size={16} />} onClick={() => setColumnsOpen(true)}>
                {copy(pageContract, "action.columns")}
              </Button>
              <Button color="primary" variant="text" size="small" startIcon={<Download size={16} />} onClick={exportCsv}>
                {copy(pageContract, "action.export", "Export")}
              </Button>
              <RowMenu
                ariaLabel={copy(pageContract, "action.more")}
                actions={[
                  { label: copy(pageContract, "action.reset_filters", "Reset filters"), icon: <X size={15} />, onSelect: clearAll, disabled: activeChips.length === 0 },
                  { label: dense ? copy(pageContract, "action.comfortable") : copy(pageContract, "action.dense"), icon: <List size={15} />, onSelect: () => setDense((d) => !d) },
                ]}
              />
            </>
          }
          summary={
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap" }}>
              {list.length !== sops.length ? (
                <Tag tone="mut">
                  {list.length} / {sops.length}
                </Tag>
              ) : null}
              {/* Template filters-result chip: soft small Chip with its own delete affordance. */}
              {activeChips.map((chip) => (
                <Chip
                  key={chip.id}
                  size="small"
                  variant="soft"
                  label={chip.label}
                  onDelete={() => {
                    chip.clear();
                    setRequestedPage(1);
                  }}
                  deleteIcon={<X aria-label={`${copy(pageContract, "action.remove_filter")}: ${chip.label}`} role="button" />}
                />
              ))}
              {activeChips.length > 0 ? (
                <Button color="primary" variant="text" size="small" onClick={clearAll}>
                  {copy(pageContract, "action.clear_all")}
                </Button>
              ) : null}
            </Box>
          }
        >
          <TextField
            select
            label={copy(pageContract, "filter.status", "Status")}
            value={statusFilter}
            onChange={({ target: { value: v } }) => {
              setStatusFilter(v);
              setRequestedPage(1);
            }}
            sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
          >
            {statusOptions.map((option) => (
              <MenuItem key={option.value} value={option.value}>
                {option.label}
              </MenuItem>
            ))}
          </TextField>
          <TextField
            select
            label={copy(pageContract, "filter.trigger")}
            value={triggerFilter}
            onChange={({ target: { value: v } }) => {
              setTriggerFilter(v);
              setRequestedPage(1);
            }}
            sx={{ minWidth: { xs: 0, sm: 160 }, flexShrink: 0, maxWidth: 1 }}
            slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
          >
            {triggerOptions.map((option) => (
              <MenuItem key={option.value} value={option.value}>
                {option.label}
              </MenuItem>
            ))}
          </TextField>
        </FilterBar>
      </div>

      {publishedSop ? (
        <Alert severity="success" className="sop-published-banner" role="status" style={{ marginBottom: 14 }} data-published-sop={publishedSop.sopId}><div style={{ flex: 1 }}>
            <b>{publishedTitle}</b>
            <div className="small" style={{ marginTop: 2 }}>{copy(pageContract, "notice.published.body")}</div>
          </div>
          <Button color="primary" variant="text" size="small" onClick={() => setPublishedDismissed(true)}>
            {copy(pageContract, "notice.published.dismiss")}
          </Button>
        </Alert>
      ) : null}

      {authRequired ? (
        <Alert severity="warning" style={{ marginBottom: 14 }}><div>{copy(pageContract, "auth.sign_in")}</div>
        </Alert>
      ) : error ? (
        <Alert severity="warning" style={{ marginBottom: 14 }}>
          {/* Farm words only (2026-09-25): this used to print the raw code in bold and the
              transport's own sentence ("backend_down Backend service is not reachable from the
              Mesha admin server."). A failed read of the library is never the reader's to fix
              field by field; the code stays in the logs. */}
          <div>{copy(pageContract, "error.load")}</div>
        </Alert>
      ) : null}

      {sops.length === 0 && !authRequired && !error ? (
        // Mock-matching empty state — the engine stands up empty; no sample cards are fabricated.
        <Card className="card" sx={{ p: { xs: 2, sm: 3 } }}>
          <div className="bd" style={{ textAlign: "center", padding: 32 }}>
            <BookText className="ic" aria-hidden="true" style={{ width: 24, height: 24, marginBottom: 10, color: "var(--brand)" }} />
	            <h3 style={{ margin: 0, fontSize: 16 }}>{copy(pageContract, "empty.title")}</h3>
            <p className="muted" style={{ maxWidth: 640, margin: "8px auto 0", lineHeight: 1.6, fontSize: 13 }}>
	              {copy(pageContract, "empty.body")}
            </p>
            <Button variant="contained" color="primary" startIcon={<Plus size={18} />} style={{ marginTop: 14 }} onClick={openBuilder}>
              {copy(pageContract, "action.new_sop")}
            </Button>
          </div>
        </Card>
      ) : (
        <>
          <div id="sopCards" style={{ display: "contents" }}>
          <div className={`kit-enter grid g3${dense ? " kit-dense" : ""}`}>
            {pagedList.map((s) => {
              const TrigIcon = s.trigger ? TRIGGER_ICON[s.trigger] : BookText;
              return (
                <div key={s.sopId} style={{ display: "grid" }}>
                  <Card
                    sx={{ cursor: "pointer" }}
                    className={`card${publishedSop && s.sopId === publishedSop.sopId ? " sop-just-published" : ""}`}
                    role="button"
                    tabIndex={0}
                    onClick={() => setDetail(s)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault();
                        setDetail(s);
                      }
                    }}
                  >
                  <div className="hd">
                    <span className="fic" style={{ width: 26, height: 26, background: "var(--brand-soft)", color: "var(--brand-d)" }}>
                      <TrigIcon className="ic" style={{ width: 14 }} />
                    </span>
                    <h3 style={{ fontSize: 14 }}>{s.name}</h3>
                  </div>
                  <div className="bd">
                    {/* Spec §9: status reads top-left, above the facet tags. */}
                    <Box sx={{ display: "flex", alignItems: "center", gap: 1, flexWrap: "wrap", mb: 1 }}>
                      <StatusTag view={s} />
                    </Box>
                    <div style={{ display: "flex", gap: 6, flexWrap: "wrap", marginBottom: 8 }}>
                      {facets.domain ? <Tag tone="mut">{s.domainLabel}</Tag> : null}
                      {facets.trigger && s.trigger ? <Tag tone="info">{s.trigger}</Tag> : null}
                      {facets.counts && s.inspectionQuestionCount > 0 ? <Tag tone="info">{s.inspectionQuestionCount} {copy(pageContract, "label.inspection_questions")}</Tag> : null}
                      {facets.counts && s.stepCount !== null ? <Tag tone="ok">{s.stepCount} {copy(pageContract, "label.steps")}</Tag> : null}
                      {facets.counts && s.followUpStepCount > 0 ? <Tag tone="info">{s.followUpStepCount} {copy(pageContract, "label.operator_steps")}</Tag> : null}
                    </div>
                    {facets.gates ? (
                      <div className="muted small">
                        {s.gates.length > 0 ? s.gates.slice(0, 3).join(" · ") : s.hasVersion ? copy(pageContract, "label.no_proof_gates") : copy(pageContract, "label.no_published_version")}
                      </div>
                    ) : null}
                    {/* Spec §9 card footer: version line + ⋮ overflow. */}
                    <Box className="sop-card-foot" onClick={(e) => e.stopPropagation()} sx={CARD_FOOT_SX}>
                      {/* The internal SOP code (procurement.feed_purchase_form) is a key, never reader copy. */}
                      {s.versionLabel ? <Box component="span" className="sop-card-meta" sx={CARD_META_SX}>{s.versionLabel}</Box> : null}
                      <Box sx={{ flex: 1 }} />
                      <RowMenu
                        ariaLabel={`${copy(pageContract, "action.more")}: ${s.name}`}
                        actions={[
                          { label: copy(pageContract, "action.view_details"), icon: <Info size={15} />, onSelect: () => setDetail(s) },
                          { label: copy(pageContract, "action.edit", "Edit"), icon: <NotebookPen size={15} />, onSelect: () => openEditor(s.sopId) },
                        ]}
                      />
                    </Box>
                  </div>
                  </Card>
                </div>
              );
            })}
            {list.length === 0 ? <div key="empty-match" className="note">{copy(pageContract, "empty.no_match")}</div> : null}
          </div>
          </div>
          {list.length > 0 ? (
            <TableFooter
              className="sop-tfoot"
              page={page}
              rowsPerPage={pageSize}
              total={list.length}
              rowsPerPageOptions={pageSizeOptions}
              onPageChange={setRequestedPage}
              onRowsPerPageChange={(n) => {
                setPageSize(n);
                setRequestedPage(1);
              }}
              left={<DenseToggle checked={dense} onChange={setDense} />}
            />
          ) : null}
        </>
      )}

      {/* Columns: which facet rows the cards carry. Presentation only. */}
      <Dialog fullWidth maxWidth="xs" open={columnsOpen} onClose={() => setColumnsOpen(false)} slotProps={{ paper: { "aria-label": copy(pageContract, "action.columns") } }}>
        <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <Columns3 className="ic" aria-hidden="true" />
          {copy(pageContract, "action.columns")}
        </DialogTitle>
        <DialogContent sx={DLG_BODY_SX}>
          {(["domain", "trigger", "counts", "gates"] as FacetId[]).map((id) => (
            <FormControlLabel
              key={id}
              sx={{ display: "flex", py: 1 }}
              control={<Switch checked={facets[id]} onChange={(e) => setFacets((f) => ({ ...f, [id]: e.target.checked }))} />}
              label={copy(pageContract, `label.${id}`, id)}
            />
          ))}
        </DialogContent>
        <DialogActions>
          <Box sx={{ flex: 1 }} />
          <Button variant="contained" color="primary" onClick={() => setColumnsOpen(false)}>{copy(pageContract, "action.close")}</Button>
        </DialogActions>
      </Dialog>

      {detail ? (
        <SopDetailModal
          view={detail}
          pageContract={pageContract}
          onClose={() => setDetail(null)}
          editPending={editorPending}
          onEdit={() => openEditor(detail.sopId)}
          onEditCapture={() => openCaptureEditor(detail.sopId)}
        />
      ) : null}
    </div>
  );
}

function SopDetailModal({ view, pageContract, onClose, onEdit, onEditCapture, editPending = false }: { view: SopCardView; pageContract: AdminUiPageContract; onClose: () => void; onEdit: () => void; onEditCapture?: () => void; editPending?: boolean }) {
  // MUI Dialog (template dialog pattern) owns the portal, Escape, the backdrop and the focus trap.
  return (
      <Dialog fullWidth maxWidth="md" open onClose={onClose} slotProps={{ paper: { "aria-label": `${copy(pageContract, "modal.detail.aria")} ${view.name}` } }}>
        <DialogTitle component="div" sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand)", width: 32, height: 32, borderRadius: 9 }}>
            <BookText className="ic" />
          </span>
          <div>
            <div className="mono muted" style={{ fontSize: 11 }}>
              SOP · {view.domainLabel.toUpperCase()}
            </div>
            <div className="b700">{view.name}</div>
          </div>
          <IconButton onClick={onClose} aria-label={copy(pageContract, "modal.detail.close_label")} sx={{ ml: "auto" }}>
            <Iconify icon="mingcute:close-line" />
          </IconButton>
        </DialogTitle>

        <DialogContent sx={DLG_BODY_SX}>
          {/* Spec §8 meta card: a titled block with an edit affordance, not a bare k/v grid. */}
          <Paper variant="outlined" sx={{ overflow: "hidden", mb: 1.5, "& .metagrid": { p: 1.75 } }}>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, px: 1.75, py: 1.25, bgcolor: "background.neutral", borderBottom: 1, borderColor: "divider", typography: "subtitle2" }}>
              <BookText className="ic" style={{ width: 14 }} aria-hidden="true" />
              <span>{copy(pageContract, "label.details")}</span>
              <Box sx={{ flex: 1 }} />
              <Button color="primary" variant="text" size="small" startIcon={<NotebookPen size={14} />} onClick={onEdit} disabled={editPending}>
                {copy(pageContract, "action.edit", "Edit")}
              </Button>
            </Box>
          <div className="metagrid">
            <div>
              <div className="k">{copy(pageContract, "label.domain")}</div>
              <div className="v">{view.domainLabel}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.trigger")}</div>
              <div className="v">{view.trigger ?? copy(pageContract, "label.placeholder")}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.code")}</div>
              <div className="v mono">{view.code}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "label.version_status")}</div>
              <div className="v">
                {view.versionLabel ?? copy(pageContract, "label.placeholder")} · {view.versionStatus ?? view.status}
              </div>
            </div>
            {view.gates.length > 0 ? (
              <div style={{ gridColumn: "1/3" }}>
                <div className="k">{copy(pageContract, "label.gates")}</div>
                <div className="v" style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                  {view.gates.map((g) => (
                    <span key={g} className="tag t-pur">
                      {g}
                    </span>
                  ))}
                </div>
              </div>
            ) : null}
          </div>
          </Paper>

          {view.description ? (
            <div className="muted small" style={{ margin: "10px 0" }}>
              {view.description}
            </div>
          ) : null}

          {/* An inspection SOP lists its load form and pages below; the generic field list would repeat the load form. */}
          {/* A general SOP has no capture form: its whole content is the operator steps below, so the
              capture section (and its "no form_dsl fields" note) is not the thing to show (PR 308 review). */}
          {view.inspectionFormDsl || view.vendorFormDsl || view.pcCareFormDsl || (view.fields.length === 0 && view.followUpStepCount > 0) ? null : (
            <>
          <div className="b700" style={{ margin: "8px 0" }}>
            {copy(pageContract, "label.steps_questions")}{" "}
            <Tag tone="mut">{view.fields.length}</Tag>
          </div>
          {view.fields.length > 0 ? (
            <div className="htl">
              {view.fields.map((f, i) => {
                const Icon = FIELD_ICON[f.type] ?? Check;
                return (
                  <div className="hrow" key={`${f.label}-${i}`}>
                    <span className="fic" style={{ width: 24, height: 24, background: "var(--bg)", color: "var(--muted)" }}>
                      <Icon className="ic" style={{ width: 13 }} />
                    </span>
                    <div className="htx">
                      <b>
                        {i + 1}. {f.label}
                      </b>
                      <div className="hmeta muted small">
                        {copy(pageContract, "label.type")}: {f.type}
                        {f.required ? ` · ${copy(pageContract, "label.required")}` : ""}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          ) : (
            <div className="note">
              {copy(pageContract, "empty.no_published_fields")}
            </div>
          )}
            </>
          )}
          {view.followUpStepCount > 0 ? <FollowUpStepsSummary pageContract={pageContract} formDsl={view.followUpFormDsl} /> : null}
          {view.inspectionFormDsl ? <InspectionSummary pageContract={pageContract} formDsl={view.inspectionFormDsl} /> : null}
          {view.vendorFormDsl ? <InspectionSummary pageContract={pageContract} formDsl={view.vendorFormDsl} profile="vendor_form" /> : null}
          {view.weighingFormDsl ? <WeighingSummary pageContract={pageContract} formDsl={view.weighingFormDsl} /> : null}
          {view.pcCareFormDsl ? <PcCareSummary pageContract={pageContract} formDsl={view.pcCareFormDsl} /> : null}
          {view.feedFormDsl ? <FeedSummary pageContract={pageContract} sopCode={view.code} formDsl={view.feedFormDsl} /> : null}
          {view.shiftingFormDsl ? <ShiftingSummary pageContract={pageContract} formDsl={view.shiftingFormDsl} /> : null}
          {isCaptureCardCode(view.code) && view.hasVersion ? <CaptureCardSummary pageContract={pageContract} sopCode={view.code} formDsl={view.followUpFormDsl} /> : null}
        </DialogContent>

        <DialogActions>
          <Button color="primary" variant="outlined" onClick={onClose}>
            {copy(pageContract, "action.close")}
          </Button>
          <div className="sp" style={{ flex: 1 }} />
          {isCaptureCardCode(view.code) && view.hasVersion && onEditCapture ? (
            <Button color="primary" variant="outlined" startIcon={<NotebookPen size={16} />} onClick={onEditCapture} disabled={editPending}>
              {copy(pageContract, "action.edit_capture_form")}
            </Button>
          ) : null}
          <Button variant="contained" color="primary" startIcon={<NotebookPen size={16} />} onClick={onEdit} disabled={editPending} loading={editPending}>
            {editPending
              ? copy(pageContract, "action.opening_editor")
              : view.followUpStepCount > 0
                ? copy(pageContract, "action.edit_operator_steps")
                : view.inspectionFormDsl
                  ? copy(pageContract, "action.edit_inspection")
                  : view.vendorFormDsl
                  ? copy(pageContract, "action.edit_vendor_form")
                  : view.feedFormDsl
                    ? copy(pageContract, "action.edit_feed")
                    : view.weighingFormDsl
                    ? copy(pageContract, "action.edit_weighing")
                    : view.pcCareFormDsl
                    ? copy(pageContract, "action.edit_pc_care")
                    : copy(pageContract, "action.new_sop_builder")}
          </Button>
        </DialogActions>
      </Dialog>
  );
}

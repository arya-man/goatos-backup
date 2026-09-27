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
  Hash,
  List,
  ListChecks,
  MapPin,
  NotebookPen,
  Plus,
  ScanLine,
  Type as TypeIcon,
  Video,
  X,
} from "lucide-react";
import { type SopCardView } from "./sop-derive";
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
import Link from "@mui/material/Link";
import Avatar from "@mui/material/Avatar";
import Divider from "@mui/material/Divider";
import Typography from "@mui/material/Typography";
import ListItemText from "@mui/material/ListItemText";
import Pagination, { paginationClasses } from "@mui/material/Pagination";
import { Label, type LabelColor } from "@/components/minimal/label";
import { EmptyContent } from "@/components/minimal/empty-content";
import TextField from "@mui/material/TextField";
import MenuItem from "@mui/material/MenuItem";
import { ShiftingSummary } from "./shifting-summary";
import { CaptureCardSummary } from "./capture-summary";
import { isCaptureCardCode } from "./capture-model";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { PageHeader } from "@/components/app/page-header";
import Switch from "@mui/material/Switch";
import FormControlLabel from "@mui/material/FormControlLabel";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import useMediaQuery from "@mui/material/useMediaQuery";
import type { Theme } from "@mui/material/styles";
import Chip from "@mui/material/Chip";
import MuiCard from "@mui/material/Card";
import Paper from "@mui/material/Paper";
import { CARDS_PER_PAGE } from "./sop-library-layout";

// The New SOP builder is a dedicated full-page surface at <module SOP page>?compose=1 — the same
// route as the module page (never a nested /new page). Legacy `?new=1` deep-links resolve to it too.

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

const STATUS_COLOR: Record<SopCardView["status"], LabelColor> = { active: "success", draft: "default", retired: "warning" };

// Dialog body rhythm: theme spacing/typography only.
const DLG_BODY_SX = {
  "& .htl > .hrow": { borderRadius: "var(--r-md)", transition: (t: Theme) => t.transitions.create("background-color") },
  "& .htl > .hrow:hover": { bgcolor: "action.hover" },
} as const;
function statusText(view: SopCardView): string {
  return view.status === "active" ? `published${view.versionNumber ? ` · v${view.versionNumber}` : ""}` : view.status;
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
  const pageSize = CARDS_PER_PAGE;
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
      {/* One column with the template gap between header, KPI row, filter card and cards (the legacy
          .screen root is display:block and its children carry no margins). */}
      <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
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


      {/* Spec §2 toolbar: search + status/trigger filters, right-aligned actions, filter chips. */}
      <div>
        <FilterBar
          bare
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
              {/* Template list toolbar: one ⋮ popover (TR1-#23). */}
              <RowMenu
                ariaLabel={copy(pageContract, "action.more")}
                actions={[
                  { label: copy(pageContract, "action.columns"), icon: <Columns3 size={15} />, onSelect: () => setColumnsOpen(true) },
                  { label: copy(pageContract, "action.export", "Export"), icon: <Download size={15} />, onSelect: exportCsv },
                  { label: copy(pageContract, "action.reset_filters", "Reset filters"), icon: <X size={15} />, onSelect: clearAll, disabled: activeChips.length === 0 },
                ]}
              />
            </>
          }
          summary={
            activeChips.length === 0 && list.length === sops.length ? undefined : (
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
            )
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
        // Template EmptyContent (job list notFound pattern) — the engine stands up empty; no sample cards are fabricated.
        <EmptyContent
          filled
          title={copy(pageContract, "empty.title")}
          description={copy(pageContract, "empty.body")}
          action={
            <Button variant="contained" color="primary" startIcon={<Plus size={18} />} sx={{ mt: 2 }} onClick={openBuilder}>
              {copy(pageContract, "action.new_sop")}
            </Button>
          }
          sx={{ py: 10 }}
        />
      ) : list.length === 0 ? (
        <EmptyContent filled title={copy(pageContract, "empty.no_match")} sx={{ py: 10 }} />
      ) : (
        <>
          {/* Template sections/job/job-list: 1/2/3-column card grid, gap 3, MUI Pagination centred below. */}
          <Box
            id="sopCards"
            sx={{ gap: 3, display: "grid", gridTemplateColumns: { xs: "repeat(1, 1fr)", sm: "repeat(2, 1fr)", md: "repeat(3, 1fr)" } }}
          >
            {pagedList.map((s) => (
              <SopItem
                key={s.sopId}
                view={s}
                facets={facets}
                pageContract={pageContract}
                justPublished={Boolean(publishedSop && s.sopId === publishedSop.sopId)}
                onView={() => setDetail(s)}
                onEdit={() => openEditor(s.sopId)}
              />
            ))}
          </Box>
          {totalPages > 1 ? (
            <Pagination
              count={totalPages}
              page={page}
              onChange={(_, value) => setRequestedPage(value)}
              sx={{ mt: { xs: 5, md: 8 }, [`& .${paginationClasses.ul}`]: { justifyContent: "center" } }}
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
      </Box>
    </div>
  );
}

type SopItemProps = {
  view: SopCardView;
  facets: Record<FacetId, boolean>;
  pageContract: AdminUiPageContract;
  justPublished: boolean;
  onView: () => void;
  onEdit: () => void;
};

// Template sections/job/job-item anatomy: ⋮ action menu pinned top-right, rounded 48px avatar,
// subtitle1 title link + caption, primary caption line, dashed divider, 2-column caption facts.
function SopItem({ view, facets, pageContract, justPublished, onView, onEdit }: SopItemProps) {
  const facts: Array<{ key: string; label: string; icon: React.ReactNode }> = [];
  if (facets.domain) facts.push({ key: "domain", label: view.domainLabel, icon: <Iconify width={16} icon="solar:tag-horizontal-bold-duotone" sx={{ flexShrink: 0 }} /> });
  if (facets.trigger && view.trigger) facts.push({ key: "trigger", label: view.trigger, icon: <Iconify width={16} icon="solar:clock-circle-bold" sx={{ flexShrink: 0 }} /> });
  if (facets.counts && view.stepCount !== null)
    facts.push({ key: "steps", label: `${view.stepCount} ${view.stepCount === 1 ? copy(pageContract, "label.step", copy(pageContract, "label.steps")) : copy(pageContract, "label.steps")}`, icon: <Iconify width={16} icon="solar:list-bold" sx={{ flexShrink: 0 }} /> });
  if (facets.counts && view.inspectionQuestionCount > 0)
    facts.push({ key: "questions", label: `${view.inspectionQuestionCount} ${copy(pageContract, "label.inspection_questions")}`, icon: <Iconify width={16} icon="solar:bill-list-bold" sx={{ flexShrink: 0 }} /> });
  if (facets.counts && view.followUpStepCount > 0)
    facts.push({ key: "operator", label: `${view.followUpStepCount} ${view.followUpStepCount === 1 ? copy(pageContract, "label.operator_step", copy(pageContract, "label.operator_steps")) : copy(pageContract, "label.operator_steps")}`, icon: <Iconify width={16} icon="solar:user-rounded-bold" sx={{ flexShrink: 0 }} /> });
  const gates = view.gates.length > 0 ? view.gates.slice(0, 3).join(" · ") : view.hasVersion ? copy(pageContract, "label.no_proof_gates") : copy(pageContract, "label.no_published_version");

  return (
    <Card
      className={justPublished ? "sop-just-published" : undefined}
      data-sop-card={view.sopId}
      sx={[{ position: "relative" }, justPublished ? (theme) => ({ boxShadow: `0 0 0 2px ${theme.vars.palette.success.main}` }) : null]}
    >
      <Box sx={{ position: "absolute", top: 8, right: 8 }}>
        <RowMenu
          ariaLabel={`${copy(pageContract, "action.more")}: ${view.name}`}
          actions={[
            { label: copy(pageContract, "action.view_details"), icon: <Iconify icon="solar:eye-bold" />, onSelect: onView },
            { label: copy(pageContract, "action.edit", "Edit"), icon: <Iconify icon="solar:pen-bold" />, onSelect: onEdit },
          ]}
        />
      </Box>

      <Box sx={{ p: 3, pb: 2 }}>
        {/* Template JobItem logo slot: a rounded 48px Avatar. An SOP has no logo, so it takes the
            template's letter fallback from its module (TR1-#33). */}
        <Avatar alt={view.domainLabel || view.name} variant="rounded" sx={{ width: 48, height: 48, mb: 2 }}>
          {(view.domainLabel || view.name).charAt(0).toUpperCase()}
        </Avatar>

        <ListItemText
          sx={{ mb: 1, pr: 3 }}
          primary={
            <Link component="button" type="button" color="inherit" underline="hover" onClick={onView} sx={{ textAlign: "left", typography: "subtitle1", minWidth: { xs: 44, md: 0 } }}>
              {view.name}
            </Link>
          }
          secondary={view.versionLabel ?? undefined}
          slotProps={{
            primary: { sx: { typography: "subtitle1" } },
            secondary: { sx: { mt: 1, typography: "caption", color: "text.disabled" } },
          }}
        />

        <Label variant="soft" color={STATUS_COLOR[view.status]}>
          {statusText(view)}
        </Label>
      </Box>

      {facts.length > 0 || facets.gates ? <Divider sx={{ borderStyle: "dashed" }} /> : null}

      {facts.length > 0 || facets.gates ? (
        <Box sx={{ p: 3, rowGap: 1.5, columnGap: 1, display: "grid", gridTemplateColumns: "repeat(2, 1fr)" }}>
          {facts.map((item) => (
            <Box key={item.key} sx={{ gap: 0.5, minWidth: 0, display: "flex", alignItems: "center", color: "text.disabled" }}>
              {item.icon}
              <Typography variant="caption" noWrap>
                {item.label}
              </Typography>
            </Box>
          ))}
          {facets.gates ? (
            <Box sx={{ gridColumn: "1 / -1", gap: 0.5, minWidth: 0, display: "flex", alignItems: "center", color: "text.disabled" }}>
              <Iconify width={16} icon="solar:shield-check-bold" sx={{ flexShrink: 0 }} />
              <Typography variant="caption" noWrap>
                {gates}
              </Typography>
            </Box>
          ) : null}
        </Box>
      ) : null}
    </Card>
  );
}

/** An open key space (form field types come from the SOP's own DSL): a missing copy key reads as
 *  words ("animal id scan"), never as the raw code "animal_id_scan" (FJ3 P1-13). */
function humanizeKey(key: string): string {
  const words = key.replace(/[_.]+/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : key;
}

function SopDetailModal({ view, pageContract, onClose, onEdit, onEditCapture, editPending = false }: { view: SopCardView; pageContract: AdminUiPageContract; onClose: () => void; onEdit: () => void; onEditCapture?: () => void; editPending?: boolean }) {
  // MUI Dialog (template dialog pattern) owns the portal, Escape, the backdrop and the focus trap.
  // Full screen on a phone: at 390/412 the md dialog clipped its right column and gate chips
  // (FJ3 P1-26). guard: sop-no-internal-codes (features/sops/sop-internal-codes.test.mjs).
  const fullScreen = useMediaQuery((theme: Theme) => theme.breakpoints.down("sm"));
  return (
      <Dialog fullWidth fullScreen={fullScreen} maxWidth="md" open onClose={onClose} slotProps={{ paper: { "aria-label": `${copy(pageContract, "modal.detail.aria")} ${view.name}` } }}>
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
          {/* Record facts on the template order-details rhythm: caption key over body2 value, one
              column on a phone. The SOP's internal code is not shown (FJ3 P1-13): the name and
              version identify it to a person. */}
          <Box sx={{ p: 1.75, display: "grid", gap: 2, gridTemplateColumns: { xs: "1fr", sm: "1fr 1fr" } }}>
            {[
              [copy(pageContract, "label.domain"), view.domainLabel],
              [copy(pageContract, "label.trigger"), view.trigger ?? copy(pageContract, "label.placeholder")],
              [copy(pageContract, "label.version_status"), `${view.versionLabel ?? copy(pageContract, "label.placeholder")} · ${view.versionStatus ?? view.status}`],
            ].map(([k, v]) => (
              <Box key={k} sx={{ minWidth: 0 }}>
                <Typography variant="caption" component="div" sx={{ color: "text.secondary" }}>{k}</Typography>
                <Typography variant="body2" sx={{ overflowWrap: "anywhere" }}>{v}</Typography>
              </Box>
            ))}
            {view.gates.length > 0 ? (
              <Box sx={{ gridColumn: "1 / -1", minWidth: 0 }}>
                <Typography variant="caption" component="div" sx={{ color: "text.secondary", mb: 0.5 }}>{copy(pageContract, "label.gates")}</Typography>
                <Stack direction="row" sx={{ flexWrap: "wrap", gap: 0.75 }}>
                  {view.gates.map((g) => (
                    <Label key={g} variant="soft" color="secondary">
                      {g}
                    </Label>
                  ))}
                </Stack>
              </Box>
            ) : null}
          </Box>
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
                        {copy(pageContract, "label.type")}: {copy(pageContract, `field_type.${f.type}`, humanizeKey(f.type))}
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

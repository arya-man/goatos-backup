"use client";

import { Tag } from "@/components/ui-primitives";

import { useCallback, useMemo, useState, useTransition } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { type SopCardView } from "./sop-derive";
import { SummaryEmpty, SummaryList, SummaryMeta, SummaryRow, SummaryTitle } from "./sop-summary";
import { FollowUpStepsSummary } from "./followup-summary";
import { InspectionSummary } from "./inspection-summary";
import { PcCareSummary } from "./pc-care-summary";
import { WeighingSummary } from "./weighing-summary";
import { FeedSummary } from "./feed-summary";
import Button from "@mui/material/Button";
import Dialog from "@mui/material/Dialog";
import DialogTitle from "@mui/material/DialogTitle";
import DialogContent from "@mui/material/DialogContent";
import DialogActions from "@mui/material/DialogActions";
import IconButton from "@mui/material/IconButton";
import { RowMenu } from "@/components/app/row-menu";
import { FilterBar } from "@/components/app/filter-bar";
import Link from "@mui/material/Link";
import Typography from "@mui/material/Typography";
import AlertTitle from "@mui/material/AlertTitle";
import { JobItem, type JobItemFact } from "@/components/app/sections/job/job-item";
import { JobList } from "@/components/app/sections/job/job-list";
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
import Avatar from "@mui/material/Avatar";
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import useMediaQuery from "@mui/material/useMediaQuery";
import type { Theme } from "@mui/material/styles";
import Chip from "@mui/material/Chip";
import Paper from "@mui/material/Paper";
import { CARDS_PER_PAGE, SOP_HEADER_LAYOUT } from "./sop-library-layout";

// The New SOP builder is a dedicated full-page surface at <module SOP page>?compose=1 — the same
// route as the module page (never a nested /new page). Legacy `?new=1` deep-links resolve to it too.

const FIELD_ICON: Record<string, IconifyName> = {
  text: "solar:pen-bold",
  number: "solar:tag-horizontal-bold-duotone",
  date_time: "solar:calendar-date-bold",
  select: "solar:list-bold",
  multiselect: "eva:done-all-fill",
  goat_lookup: "solar:user-id-bold",
  animal_id_scan: "solar:user-id-bold",
  location_picker: "mingcute:location-fill",
  photo_proof: "solar:camera-add-bold",
  video_proof: "solar:videocamera-record-bold",
};

type FacetId = "domain" | "trigger" | "counts" | "gates";

/** Title-cases the module segment of e.g. "/counts/sops" for the breadcrumb trail. */
function moduleSegment(basePath: string): string {
  const seg = basePath.split("/").filter(Boolean)[0] ?? "";
  return seg ? seg.charAt(0).toUpperCase() + seg.slice(1) : "";
}

const STATUS_COLOR: Record<SopCardView["status"], LabelColor> = { active: "success", draft: "default", retired: "warning" };

// Dialog body rhythm: theme spacing/typography only.
/** The card page's search param (template JobList pagination links). */
const PAGE_PARAM = "page";

const DLG_BODY_SX = {
  "& .htl > .hrow": { borderRadius: 1, transition: (t: Theme) => t.transitions.create("background-color") },
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
  // The card page is the URL's `page` (template JobList pagination: each page item is a link, so a
  // deep link opens that page). The whole list is already on the client, so a page change only
  // rewrites the URL with history.replaceState (Next syncs useSearchParams): no server round trip.
  // A filter change starts again at page 1 by dropping the param in place. guard: sop-paging-client-only
  const searchParams = useSearchParams();
  const pathname = usePathname();
  // Below md the folded toolbar leaves the search ~130px (search + Filters + ⋮ on one row), so the
  // placeholder is the short search label there; the long hint only fits at md+.
  // guard: r2 text-fit|placeholder-clipped (P0)
  const foldedToolbar = useMediaQuery((theme: Theme) => theme.breakpoints.down("md"));
  const requestedPage = Math.max(1, Number(searchParams.get(PAGE_PARAM)) || 1);
  const pageHref = (n: number) => {
    const next = new URLSearchParams(searchParams.toString());
    if (n <= 1) next.delete(PAGE_PARAM);
    else next.set(PAGE_PARAM, String(n));
    const qs = next.toString();
    return qs ? `${pathname}?${qs}` : pathname;
  };
  const setRequestedPage = (n: number) => {
    if (n === requestedPage) return;
    window.history.replaceState(window.history.state, "", pageHref(n));
  };
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
  const clearAll = () => {
    setQuery("");
    setStatusFilter("");
    setTriggerFilter("");
    setRequestedPage(1);
  };

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
    // The module page mounts this below its own root, so the page grid's gap does not reach it: the
    // column stacks header, toolbar and cards with the template gap itself.
    <Box sx={{ display: "flex", flexDirection: "column", gap: 3 }}>
      <PageHeader
        layout={SOP_HEADER_LAYOUT}
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb", moduleSegment(basePath)) || moduleSegment(basePath) }, { label: pageContract.title }]}
        actions={
          <Box sx={{ display: "flex", alignItems: "center", justifyContent: "flex-end", gap: 1, flexWrap: "wrap", "& .wt-assumptions-control": { display: "contents" } }}>
            {extraNode}
            <Button variant="contained" color="primary" startIcon={<Iconify icon="mingcute:add-line" />} onClick={openBuilder}>
              {copy(pageContract, "action.new_sop")}
            </Button>
          </Box>
        }
      />


      {/* Spec §2 toolbar: search + status/trigger filters, right-aligned actions, filter chips. */}
      <div>
        <FilterBar
          bare
          fold={{ label: copy(pageContract, "action.filters", "Filters"), count: activeChips.filter((chip) => chip.id !== "q").length }}
          search={{
            value: query,
            placeholder: copy(pageContract, foldedToolbar ? "filter.search_label" : "filter.search_placeholder"),
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
                  { label: copy(pageContract, "action.columns"), icon: <Iconify icon="ic:round-view-module" width={16} />, onSelect: () => setColumnsOpen(true) },
                  { label: copy(pageContract, "action.export", "Export"), icon: <Iconify icon="solar:download-bold" width={16} />, onSelect: exportCsv },
                  { label: copy(pageContract, "action.reset_filters", "Reset filters"), icon: <Iconify icon="mingcute:close-line" width={16} />, onSelect: clearAll, disabled: activeChips.length === 0 },
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
                  deleteIcon={<Iconify icon="mingcute:close-line" aria-label={`${copy(pageContract, "action.remove_filter")}: ${chip.label}`} role="button" />}
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
        <Alert
          severity="success"
          role="status"
          data-published-sop={publishedSop.sopId}
          action={
            <Button color="inherit" size="small" onClick={() => setPublishedDismissed(true)}>
              {copy(pageContract, "notice.published.dismiss")}
            </Button>
          }
        >
          <AlertTitle>{publishedTitle}</AlertTitle>
          {copy(pageContract, "notice.published.body")}
        </Alert>
      ) : null}

      {authRequired ? (
        <Alert severity="warning">{copy(pageContract, "auth.sign_in")}</Alert>
      ) : error ? (
        <Alert severity="warning">
          {/* Farm words only (2026-09-25): this used to print the raw code in bold and the
              transport's own sentence ("backend_down Backend service is not reachable from the
              Mesha admin server."). A failed read of the library is never the reader's to fix
              field by field; the code stays in the logs. */}
          {copy(pageContract, "error.load")}
        </Alert>
      ) : null}

      {sops.length === 0 && !authRequired && !error ? (
        // Template EmptyContent (job list notFound pattern) — the engine stands up empty; no sample cards are fabricated.
        <EmptyContent
          filled
          title={copy(pageContract, "empty.title")}
          description={copy(pageContract, "empty.body")}
          action={
            <Button variant="contained" color="primary" startIcon={<Iconify icon="mingcute:add-line" />} sx={{ mt: 2 }} onClick={openBuilder}>
              {copy(pageContract, "action.new_sop")}
            </Button>
          }
          sx={{ py: 10 }}
        />
      ) : list.length === 0 ? (
        <EmptyContent filled title={copy(pageContract, "empty.no_match")} sx={{ py: 10 }} />
      ) : (
        // Template sections/job/job-list: the 1/2/3-column JobItem grid with the centred pagination.
        // data-testid="sop-cards" is the e2e hook (scripts/sop-builder-e2e.mjs; the old #sopCards id is gone).
        <Box data-testid="sop-cards" sx={{ display: "contents" }}>
        <JobList pagination={{ page, hrefs: Array.from({ length: totalPages }, (_, i) => pageHref(i + 1)), onSelect: setRequestedPage }}>
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
        </JobList>
        </Box>
      )}

      {/* Columns: which facet rows the cards carry. Presentation only. */}
      <Dialog fullWidth maxWidth="xs" open={columnsOpen} onClose={() => setColumnsOpen(false)} slotProps={{ paper: { "aria-label": copy(pageContract, "action.columns") } }}>
        <DialogTitle sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <Iconify icon="ic:round-view-module" aria-hidden="true" />
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
  const facts: JobItemFact[] = [];
  if (facets.domain) facts.push({ key: "domain", label: view.domainLabel, icon: <Iconify width={16} icon="solar:tag-horizontal-bold-duotone" sx={{ flexShrink: 0 }} /> });
  if (facets.trigger && view.trigger) facts.push({ key: "trigger", label: view.trigger, icon: <Iconify width={16} icon="solar:clock-circle-bold" sx={{ flexShrink: 0 }} /> });
  if (facets.counts && view.stepCount !== null)
    facts.push({ key: "steps", label: `${view.stepCount} ${view.stepCount === 1 ? copy(pageContract, "label.step", copy(pageContract, "label.steps")) : copy(pageContract, "label.steps")}`, icon: <Iconify width={16} icon="solar:list-bold" sx={{ flexShrink: 0 }} /> });
  if (facets.counts && view.inspectionQuestionCount > 0)
    facts.push({ key: "questions", label: `${view.inspectionQuestionCount} ${copy(pageContract, "label.inspection_questions")}`, icon: <Iconify width={16} icon="solar:bill-list-bold" sx={{ flexShrink: 0 }} /> });
  if (facets.counts && view.followUpStepCount > 0)
    facts.push({ key: "operator", label: `${view.followUpStepCount} ${view.followUpStepCount === 1 ? copy(pageContract, "label.operator_step", copy(pageContract, "label.operator_steps")) : copy(pageContract, "label.operator_steps")}`, icon: <Iconify width={16} icon="solar:user-rounded-bold" sx={{ flexShrink: 0 }} /> });
  const gates = view.gates.length > 0 ? view.gates.slice(0, 3).join(" · ") : view.hasVersion ? copy(pageContract, "label.no_proof_gates") : copy(pageContract, "label.no_published_version");

  if (facets.gates) facts.push({ key: "gates", label: gates, icon: <Iconify width={16} icon="solar:shield-check-bold" sx={{ flexShrink: 0 }} /> });

  // Template JobItem through its slots: the logo slot is an SOP icon tile, not a letter avatar (an
  // SOP has no logo; TR1-#33, TR2-P2-10; guard: sop-card-logo-tile), the facts WRAP (no "Verify b…"), the title opens the detail, the version is the "posted" line, the status Label is the
  // meta line, the facet captions are the fact grid and View / Edit the card's ⋮ menu.
  return (
    <JobItem
      className={justPublished ? "sop-just-published" : undefined}
      data-sop-card={view.sopId}
      sx={[{ position: "relative" }, justPublished ? (theme) => ({ boxShadow: `0 0 0 2px ${theme.vars.palette.success.main}` }) : null]}
      avatar={<Iconify width={28} icon="solar:bill-list-bold-duotone" sx={{ color: "primary.main" }} aria-hidden />}
      wrapFacts
      title={
        <Link component="button" type="button" color="inherit" underline="hover" onClick={onView} sx={{ textAlign: "left", typography: "subtitle1", minWidth: { xs: 44, md: 0 } }}>
          {view.name}
        </Link>
      }
      secondary={view.versionLabel ?? undefined}
      meta={
        <Label variant="soft" color={STATUS_COLOR[view.status]}>
          {statusText(view)}
        </Label>
      }
      facts={facts}
      menuLabel={`${copy(pageContract, "action.more")}: ${view.name}`}
      menuActions={[
        { key: "view", label: copy(pageContract, "action.view_details"), icon: <Iconify icon="solar:eye-bold" />, onClick: onView },
        { key: "edit", label: copy(pageContract, "action.edit", "Edit"), icon: <Iconify icon="solar:pen-bold" />, onClick: onEdit },
      ]}
    />
  );
}

/** An open key space (form field types come from the SOP's own DSL): a missing copy key reads as
 *  words ("animal id scan"), never as the raw code "animal_id_scan" (FJ3 P1-13). */
function humanizeKey(key: string): string {
  const words = key.replace(/[_.]+/g, " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : key;
}

/** The field-type glyph beside a published field row (template soft avatar). */
function FieldGlyph({ icon }: { icon: IconifyName }) {
  return (
    <Avatar variant="rounded" aria-hidden="true" sx={{ width: 24, height: 24, bgcolor: "background.neutral", color: "text.secondary" }}>
      <Iconify icon={icon} width={13} />
    </Avatar>
  );
}

function SopDetailModal({ view, pageContract, onClose, onEdit, onEditCapture, editPending = false }: { view: SopCardView; pageContract: AdminUiPageContract; onClose: () => void; onEdit: () => void; onEditCapture?: () => void; editPending?: boolean }) {
  // MUI Dialog (template dialog pattern) owns the portal, Escape, the backdrop and the focus trap.
  // Full screen on a phone: at 390/412 the md dialog clipped its right column and gate chips
  // (FJ3 P1-26). guard: sop-no-internal-codes (features/sops/sop-internal-codes.test.mjs).
  const fullScreen = useMediaQuery((theme: Theme) => theme.breakpoints.down("sm"));
  return (
      <Dialog fullWidth fullScreen={fullScreen} maxWidth="md" open onClose={onClose} slotProps={{ paper: { "aria-label": `${copy(pageContract, "modal.detail.aria")} ${view.name}` } }}>
        <DialogTitle component="div" sx={{ display: "flex", alignItems: "center", gap: 1.5 }}>
          <Avatar variant="rounded" aria-hidden="true" sx={{ width: 32, height: 32, bgcolor: "primary.lighter", color: "primary.dark" }}>
            <Iconify icon="solar:notebook-bold-duotone" width={18} />
          </Avatar>
          <div>
            <Typography variant="caption" component="div" sx={{ color: "text.secondary", fontFamily: "monospace" }}>
              SOP · {view.domainLabel.toUpperCase()}
            </Typography>
            <Typography variant="subtitle1" component="div">
              {view.name}
            </Typography>
          </div>
          <IconButton onClick={onClose} aria-label={copy(pageContract, "modal.detail.close_label")} sx={{ ml: "auto" }}>
            <Iconify icon="mingcute:close-line" />
          </IconButton>
        </DialogTitle>

        <DialogContent sx={DLG_BODY_SX}>
          {/* Spec §8 meta card: a titled block with an edit affordance, not a bare k/v grid. */}
          <Paper variant="outlined" sx={{ overflow: "hidden", mb: 1.5, "& .metagrid": { p: 1.75 } }}>
            <Box sx={{ display: "flex", alignItems: "center", gap: 1, px: 1.75, py: 1.25, bgcolor: "background.neutral", borderBottom: 1, borderColor: "divider", typography: "subtitle2" }}>
              <Iconify icon="solar:notebook-bold-duotone" width={14} aria-hidden="true" />
              <span>{copy(pageContract, "label.details")}</span>
              <Box sx={{ flex: 1 }} />
              <Button color="primary" variant="text" size="small" startIcon={<Iconify icon="solar:pen-bold" width={14} />} onClick={onEdit} disabled={editPending}>
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
            <Typography variant="body2" sx={{ color: "text.secondary", my: 1.25 }}>
              {view.description}
            </Typography>
          ) : null}

          {/* An inspection SOP lists its load form and pages below; the generic field list would repeat the load form. */}
          {/* A general SOP has no capture form: its whole content is the operator steps below, so the
              capture section (and its "no form_dsl fields" note) is not the thing to show (PR 308 review). */}
          {view.inspectionFormDsl || view.vendorFormDsl || view.pcCareFormDsl || (view.fields.length === 0 && view.followUpStepCount > 0) ? null : (
            <>
          <Typography variant="subtitle2" component="div" sx={{ my: 1 }}>
            {copy(pageContract, "label.steps_questions")}{" "}
            <Tag tone="mut">{view.fields.length}</Tag>
          </Typography>
          {view.fields.length > 0 ? (
            <SummaryList>
              {view.fields.map((f, i) => (
                <SummaryRow key={`${f.label}-${i}`} lead={<FieldGlyph icon={FIELD_ICON[f.type] ?? "eva:checkmark-fill"} />}>
                  <SummaryTitle>
                    {i + 1}. {f.label}
                  </SummaryTitle>
                  <SummaryMeta>
                    {copy(pageContract, "label.type")}: {copy(pageContract, `field_type.${f.type}`, humanizeKey(f.type))}
                    {f.required ? ` · ${copy(pageContract, "label.required")}` : ""}
                  </SummaryMeta>
                </SummaryRow>
              ))}
            </SummaryList>
          ) : (
            <SummaryEmpty>{copy(pageContract, "empty.no_published_fields")}</SummaryEmpty>
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
          <Box sx={{ flex: 1 }} />
          {isCaptureCardCode(view.code) && view.hasVersion && onEditCapture ? (
            <Button color="primary" variant="outlined" startIcon={<Iconify icon="solar:pen-bold" width={16} />} onClick={onEditCapture} disabled={editPending}>
              {copy(pageContract, "action.edit_capture_form")}
            </Button>
          ) : null}
          <Button variant="contained" color="primary" startIcon={<Iconify icon="solar:pen-bold" width={16} />} onClick={onEdit} disabled={editPending} loading={editPending}>
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

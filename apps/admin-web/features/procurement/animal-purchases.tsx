import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { EmptyState } from "@/components/app/empty-state";
import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { AnimalPurchaseRecordedRange } from "./animal-purchase-recorded-range";
import { KpiValue } from "./kpi-value";
import type { ReactNode } from "react";
import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { CircleCheck, CircleX, Hourglass, Truck, Video } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getAnimalPurchaseDeskCounts, listAnimalPurchaseLoads, listAnimalPurchaseReview } from "@/lib/api/procurement-server";
import type { AnimalPurchaseAnimal, AnimalPurchaseLoad } from "@/lib/api/procurement";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { Tag, type Tone } from "@/components/ui-primitives";
import {
  control,
  controlEnabled,
  copy,
  optionGroup,
  table,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import { fmtDate, fmtDateTime } from "@/lib/format";
import { num } from "./sales-format";
import { AnimalPurchaseDecisionForm } from "./animal-purchase-decision-form";
import { AnimalPurchaseTelemetry } from "./animal-purchase-telemetry";
import { AnimalPurchaseAnswers, AnimalPurchaseMedia, FieldVerdictChip, type SopCopy } from "./animal-purchase-sop";
import { AnimalPurchaseLightbox } from "./animal-purchase-lightbox";
import { FormSelect } from "./form-select";
import { ProcurementTableFooter } from "./table-footer-links";
import { listOptions } from "./option-utils";
import Alert from "@mui/material/Alert";
import Button from "@mui/material/Button";
import Box from "@mui/material/Box";
import { phoneLoadCardsSx } from "./procurement-sx";

const LOAD_CARDS_SX = phoneLoadCardsSx("animal-purchase-loads-table", [{ nth: 1, column: "1", row: 1 }, { nth: 3, column: "2", row: 1, alignEnd: true }, { nth: 2, column: "1", row: 2, secondary: true }, { nth: 6, column: "2", row: 2, alignEnd: true }]);

const PATHNAME = "/procurement/animal-purchases";
const DEFAULT_DECISION = "pending";
const DEFAULT_LIMIT = 20;
const DECISION_CONTROL = "decide_animal_purchase";

function hrefWithQuery(sp: RouteSearchParams, patch: Record<string, string | null>): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  for (const [key, value] of Object.entries(patch)) {
    if (value === null || value === "") query.delete(key);
    else query.set(key, value);
  }
  const qs = query.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

// decision_tone is backend vocabulary (neutral | ok | bad); the mock palette names differ, so the
// mapping lives here once. An unknown tone renders muted rather than inventing a colour.
function decisionTone(tone: AnimalPurchaseAnimal["decision_tone"]): Tone {
  if (tone === "ok") return "ok";
  if (tone === "bad") return "dng";
  return "mut";
}

/**
 * Animal purchases — the CEO/CXO's review of the animals the buying desk filmed on the phone
 * (maintainer decision 2026-09-13).
 *
 * Three bounded reads, in parallel: one page of loads, one page of the review queue for the
 * selected load + decision filter, and the WHOLE-DESK counts for the header tiles. The page never
 * records a load or an animal; its only write is the decision, and that renders only behind the
 * backend-declared `decide_animal_purchase` control — there is deliberately no role check here.
 */
export async function AnimalPurchasesPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // The decision filter is validated against the SERVED option keys, never trusted raw.
  const decisionOptions = optionGroup(pageContract, "animal_purchase_decisions");
  const requestedDecision = one(sp, "decision");
  const decision = decisionOptions.some((option) => option.key === requestedDecision) && requestedDecision
    ? requestedDecision
    : DEFAULT_DECISION;
  const loadId = one(sp, "load_id") || undefined;
  // Recorded-on window: ISO dates only reach the read; anything else is dropped, not guessed.
  const isoDate = (raw: string | undefined) => (raw && /^\d{4}-\d{2}-\d{2}$/.test(raw) ? raw : "");
  const recordedFrom = isoDate(one(sp, "recorded_from"));
  const recordedTo = isoDate(one(sp, "recorded_to"));
  const animalCursor = one(sp, "ap_cursor") || undefined;
  const loadCursor = one(sp, "ld_cursor") || undefined;

  const loadsTable = table(pageContract, "animal-purchase-loads");
  const animalsTable = table(pageContract, "animal-purchase-animals");
  // Rows per page for the loads table: a contract page size chosen in the URL (`ld_limit`), else the
  // first. `ld_offset` is only the position shown in the footer range ("21–40 of …"); the read
  // itself stays keyset-paged by `ld_cursor`.
  const loadsPageSizes = loadsTable.page_size_options.length > 0 ? loadsTable.page_size_options : [DEFAULT_LIMIT];
  const requestedLoadsLimit = Number(one(sp, "ld_limit"));
  const loadsLimit = loadsPageSizes.includes(requestedLoadsLimit) ? requestedLoadsLimit : loadsPageSizes[0];
  const loadsOffsetRaw = Number(one(sp, "ld_offset"));
  const loadsOffset = loadCursor && Number.isInteger(loadsOffsetRaw) && loadsOffsetRaw > 0 ? loadsOffsetRaw : 0;
  const animalsLimit = animalsTable.page_size_options[0] ?? DEFAULT_LIMIT;

  const [loadsResult, reviewResult, totalsResult] = await Promise.all([
    listAnimalPurchaseLoads({ limit: loadsLimit, cursor: loadCursor }),
    listAnimalPurchaseReview({
      load_id: loadId,
      decision,
      recorded_from: recordedFrom || undefined,
      recorded_to: recordedTo || undefined,
      limit: animalsLimit,
      cursor: animalCursor,
    }),
    // The header tiles count the WHOLE desk ("across every load"), which the filtered queue read
    // cannot answer once a load is selected.
    getAnimalPurchaseDeskCounts(),
  ]);
  if (firstAuthRequiredError(loadsResult, reviewResult, totalsResult)) redirect(INTERNAL_LOGIN_PATH);

  const loads: AnimalPurchaseLoad[] = loadsResult.ok ? listOrEmpty(loadsResult.data.loads) : [];
  const loadsNextCursor = loadsResult.ok ? loadsResult.data.next_cursor : undefined;
  const animals: AnimalPurchaseAnimal[] = reviewResult.ok ? listOrEmpty(reviewResult.data.animals) : [];
  const animalsNextCursor = reviewResult.ok ? reviewResult.data.next_cursor : undefined;
  const filters = reviewResult.ok ? listOrEmpty(reviewResult.data.filters) : [];
  const totals = totalsResult.ok ? totalsResult.data : null;

  const none = copy(pageContract, "value.none");
  const canDecide = controlEnabled(pageContract, DECISION_CONTROL, false);
  const decideDisabledReason = pageContract.controls.find((item) => item.id === DECISION_CONTROL)
    ? control(pageContract, DECISION_CONTROL).disabled_reason
    : undefined;
  const loadColumns = tableLabels(pageContract, "animal-purchase-loads");
  const animalColumn = (key: string) => copy(pageContract, `column.${key}`);
  const selectedLoad = loadId ? loads.find((load) => load.load_id === loadId) : undefined;
  const selectedLoadRef = selectedLoad?.load_ref ?? animals.find((animal) => animal.load_id === loadId)?.load_ref;

  const feedback = { status: one(sp, "ap_status"), code: one(sp, "ap_code") };
  const feedbackText = feedback.status
    ? copy(pageContract, `action.${feedback.code ?? ""}`, "") || copy(pageContract, "action.error_form")
    : "";

  // Questionnaire copy. Every key is served by the backend map; a contract older than this
  // screen falls through to the route's COPY_FALLBACKS in admin-ui-contract.
  const sopCopy: SopCopy = {
    mediaTitle: copy(pageContract, "media.title"),
    mediaEmpty: copy(pageContract, "media.empty"),
    photoOpen: copy(pageContract, "photo.open"),
    close: copy(pageContract, "action.close"),
    answersTitle: copy(pageContract, "answers.title"),
    verdictSection: copy(pageContract, "verdict.section"),
    attentionHint: copy(pageContract, "attention.hint"),
    fieldVerdictHint: copy(pageContract, "field_verdict.hint"),
  };

  const decisionLabels = {
    title: copy(pageContract, "decision.title"),
    note: copy(pageContract, "decision.note"),
    noteHint: copy(pageContract, "decision.note_hint"),
    accept: copy(pageContract, "action.accept"),
    reject: copy(pageContract, "action.reject"),
    deciding: copy(pageContract, "action.deciding"),
    // The sentences the in-place decision shows beside the buttons, one per outcome code.
    outcomes: Object.fromEntries(
      ["decided_accepted", "decided_rejected", "decide_conflict", "decide_failed", "error_form"].map((code) => [code, copy(pageContract, `action.${code}`)]),
    ),
  };

  return (
    <div className="screen on">
      <AnimalPurchaseTelemetry rows={animals.length} pending={totals?.pending ?? 0} feedback={feedback} />

      <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb"), href: "/procurement/source-entry" }, { label: pageContract.title }]} />

      {/* Decision feedback from the Server Action's redirect. Every code resolves to page copy;
          an unknown one falls back to the generic failure line rather than leaking the token. */}
      {feedback.status ? (
        <Alert severity={feedback.status === "success" ? "info" : "error"} style={{ marginBottom: 14 }} role="status">
          {feedbackText}
        </Alert>
      ) : null}

      {!loadsResult.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {loadsResult.error.message}
        </Alert>
      ) : null}
      {!reviewResult.ok ? (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          {reviewResult.error.message}
        </Alert>
      ) : null}

      {/* Whole-desk figures from the backend counts, never sums over the rendered page. */}
      <Box sx={{ mb: 1.75 }}>
      <KpiGrid>
        <KpiCard
          tone="primary"
          label={copy(pageContract, "summary.loads")}
          // The loads read is one keyset page; a trailing "+" says there are more than shown.
          value={loadsResult.ok ? `${num(loads.length)}${loadsNextCursor ? "+" : ""}` : none}
          icon={<Truck aria-hidden="true" />}
          hint={loadsTable.title}
        />
        <KpiCard
          tone="warning"
          label={copy(pageContract, "summary.pending")}
          value={totals ? <KpiValue value={totals.pending} /> : none}
          icon={<Hourglass aria-hidden="true" />}
        />
        <KpiCard
          tone="success"
          label={copy(pageContract, "summary.accepted")}
          value={totals ? <KpiValue value={totals.accepted} /> : none}
          icon={<CircleCheck aria-hidden="true" />}
        />
        <KpiCard
          tone="error"
          label={copy(pageContract, "summary.rejected")}
          value={totals ? <KpiValue value={totals.rejected} /> : none}
          icon={<CircleX aria-hidden="true" />}
        />
      </KpiGrid>
      </Box>

      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.loads.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          {/* The load FILTER: a plain query-param link, not an overlay. Clicking a load narrows
              the animals section below; the all-loads chip clears it. */}
          {/* The load FILTER as one pill strip: All loads · <selected>. Query-param links, no overlay. */}
          <AnimatedTabs
            variant="pill"
            ariaLabel={copy(pageContract, "filter.load")}
            value={loadId ? "selected" : "all"}
            items={[
              { value: "all", label: copy(pageContract, "filter.load.all"), href: hrefWithQuery(sp, { load_id: null, ap_cursor: null, ap_status: null, ap_code: null }) },
              ...(loadId ? [{ value: "selected", label: selectedLoadRef ?? none, href: hrefWithQuery(sp, {}) }] : []),
            ]}
          />
        </div>
        {/* The selected load's own record: what the buying desk typed when it opened the load on
            the phone (load number, vendor, farm, expected count, note), who recorded it and when,
            plus any EXTRA authored SOP answers (how it arrived, documents, ...). These sit with the
            load, not with any one animal, so the CEO reads them once here. A recorder whose roster
            name cannot be resolved is dropped, never shown as an id. */}
        {selectedLoad ? (
          <div className="ap-load-answers" data-testid="ap-load-detail" aria-label={copy(pageContract, "label.load_answers")}>
            <span className="muted small b700">{copy(pageContract, "label.load_answers")}</span>
            <span className="ap-load-answer">
              <span className="muted small">{copy(pageContract, "column.load_ref")}</span> <b>{selectedLoad.load_ref}</b>
            </span>
            <span className="ap-load-answer">
              <span className="muted small">{copy(pageContract, "column.vendor_name")}</span> <b>{selectedLoad.vendor_name || none}</b>
            </span>
            <span className="ap-load-answer">
              <span className="muted small">{copy(pageContract, "column.farm")}</span> <b>{selectedLoad.farm}</b>
            </span>
            <span className="ap-load-answer">
              <span className="muted small">{copy(pageContract, "column.expected_count")}</span> <b>{num(selectedLoad.expected_count)}</b>
            </span>
            <span className="ap-load-answer">
              <span className="muted small">{copy(pageContract, "label.load.status")}</span>{" "}
              <b>{copy(pageContract, `status.load.${selectedLoad.status}`, selectedLoad.status)}</b>
            </span>
            {selectedLoad.recorded_by_name ? (
              <span className="ap-load-answer">
                <span className="muted small">{copy(pageContract, "label.load.recorded_by")}</span> <b>{selectedLoad.recorded_by_name}</b>
              </span>
            ) : null}
            <span className="ap-load-answer">
              <span className="muted small">{copy(pageContract, "label.load.added_on")}</span> <b>{fmtDateTime(selectedLoad.created_at)}</b>
            </span>
            {(selectedLoad.answer_rows ?? []).map((row) => (
              <span key={row.question_id} className="ap-load-answer">
                <span className="muted small">{row.question}</span> <b>{row.answer}</b>
              </span>
            ))}
            <span className="ap-load-answer ap-load-note">
              <span className="muted small">{copy(pageContract, "label.load.notes")}</span>{" "}
              {selectedLoad.notes ? <b>{selectedLoad.notes}</b> : <span className="muted">{copy(pageContract, "label.load.no_notes")}</span>}
            </span>
          </div>
        ) : null}

        {loads.length === 0 ? (
          <EmptyState title={copy(pageContract, "empty.loads")} />
        ) : (
          <div id="animal-purchase-loads" className="twrap" tabIndex={0} role="region" aria-label={loadsTable.title}>
            <Box sx={LOAD_CARDS_SX}>
              <Table className="animal-purchase-loads-table" aria-label={loadsTable.title}>
                <TableHead>
                  {/* Header labels come from the page contract IN ITS ORDER; the body cells below
                      are written in that same order (load_ref, vendor_name, farm, expected_count,
                      total, pending, accepted, rejected, created_at). */}
                  <TableRow>
                    {loadColumns.map((label) => (
                      <TableCell component="th" key={label}>{label}</TableCell>
                    ))}
                  </TableRow>
                </TableHead>
                <TableBody>
                  {loads.map((load) => {
                    const selected = load.load_id === loadId;
                    const filterHref = hrefWithQuery(sp, {
                      load_id: selected ? null : load.load_id,
                      ap_cursor: null,
                      ap_status: null,
                      ap_code: null,
                    });
                    const cellLink = (content: ReactNode) => (
                      <Link href={filterHref} className="celllink" scroll={false} aria-current={selected ? "true" : undefined}>
                        {content}
                      </Link>
                    );
                    return (
                      <TableRow key={load.load_id} className={selected ? "on" : undefined} aria-selected={selected ? "true" : undefined}>
                        <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(<b>{load.load_ref}</b>)}</TableCell>
                        <TableCell>{cellLink(load.vendor_name || none)}</TableCell>
                        <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(load.farm)}</TableCell>
                        <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.expected_count))}</TableCell>
                        <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.counts.total))}</TableCell>
                        <TableCell style={{ whiteSpace: "nowrap" }}>
                          {cellLink(<Tag tone={load.counts.pending > 0 ? "warn" : "mut"}>{num(load.counts.pending)}</Tag>)}
                        </TableCell>
                        <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.counts.accepted))}</TableCell>
                        <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.counts.rejected))}</TableCell>
                        <TableCell style={{ whiteSpace: "nowrap" }}>{cellLink(fmtDate(load.created_at))}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </Box>
          </div>
        )}

        {loads.length > 0 ? (
          /* Keyset cursors read forward only; "Back" returns to the first page rather than growing
             a trail — the loads list is expected to stay shallow. There is no row total, so the range
             reads "1–20 of more than 20" while a next page exists and "1–2 of 2" once it does not. */
          <ProcurementTableFooter
            denseLabel={copy(pageContract, "action.dense", "Dense")}
            rowsLabel={copy(pageContract, "pager.rows_per_page")}
            rowsValue={loadsLimit}
            rowsOptions={loadsPageSizes.map((size) => ({ size, href: hrefWithQuery(sp, { ld_limit: String(size), ld_cursor: null, ld_offset: null }) }))}
            page={loadCursor ? 2 : 1}
            pageCount={loadsNextCursor ? (loadCursor ? 3 : 2) : loadCursor ? 2 : 1}
            prevHref={loadCursor ? hrefWithQuery(sp, { ld_cursor: null, ld_offset: null }) : null}
            nextHref={loadsNextCursor ? hrefWithQuery(sp, { ld_cursor: loadsNextCursor, ld_offset: String(loadsOffset + loads.length) }) : null}
            rangeLabel={`${num(loadsOffset + 1)}–${num(loadsOffset + loads.length)} ${copy(pageContract, "pager.of", "of")} ${
              loadsNextCursor ? `${copy(pageContract, "pager.more_than")} ${num(loadsOffset + loads.length)}` : num(loadsOffset + loads.length)
            }`}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
            denseTargetId="animal-purchase-loads"
          />
        ) : null}
      </section>

      <section className="card">
        <div className="hd">
          <Video className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.animals.title")}</h3>
        </div>

        {/* Load and recorded-on window: a plain GET form, so the filter lives in the URL like
            every other list filter and the whole-filter chip counts follow it. The chip and the
            page cursor are dropped on submit by construction (they are not form fields). */}
        {/* The fields sit straight on the card (template invoice toolbar, no inner bordered box); the
            top inset keeps the notched "Load" / "Recorded from" labels clear of the card edge. */}
        <Box
          component="form"
          method="get"
          action={PATHNAME}
          className="ap-filter-bar"
          role="search"
          aria-label={copy(pageContract, "filter.load")}
          sx={{
            "&&": { pt: 1.25, px: 0, pb: 0, border: 0, borderRadius: 0, background: "none" },
            "& .kit-daterange.ap-filter-date": { height: "auto", p: 0, border: 0, borderRadius: 0, background: "none", flex: "1 1 20rem", minWidth: "17.5rem" },
          }}
        >
          {decision !== DEFAULT_DECISION ? <input type="hidden" name="decision" value={decision} /> : null}
          <FormSelect
            label={copy(pageContract, "filter.load")}
            name="load_id"
            defaultValue={loadId ?? ""}
            className="ap-filter"
            minWidth={220}
            options={listOptions(
              loads,
              (load) => load.load_id,
              (load) => `${load.load_ref} · ${load.vendor_name}`,
              copy(pageContract, "filter.load.all"),
            )}
          />
          <AnimalPurchaseRecordedRange
            from={recordedFrom}
            to={recordedTo}
            label={copy(pageContract, "filter.recorded_from")}
            fromLabel={copy(pageContract, "filter.from")}
            toLabel={copy(pageContract, "filter.recorded_to")}
            previousMonthLabel={copy(pageContract, "date.prev_month", "Previous month")}
            nextMonthLabel={copy(pageContract, "date.next_month", "Next month")}
          />
          <Button type="submit" variant="contained" color="primary" size="small">
            {copy(pageContract, "filter.apply")}
          </Button>
          {loadId || recordedFrom || recordedTo ? (
            <Link href={hrefWithQuery(sp, { load_id: null, recorded_from: null, recorded_to: null, ap_cursor: null, ap_status: null, ap_code: null })} className="btn sm" scroll={false}>
              {copy(pageContract, "filter.clear")}
            </Link>
          ) : null}
        </Box>

        {/* Decision chips are the response's own filters: label and WHOLE-FILTER count verbatim,
            selection as the backend reports it. A filter switch drops the cursor by construction. */}
        {filters.length > 0 ? (
          <Box sx={{ mb: 1.75 }}>
            <AnimatedTabs
              ariaLabel={copy(pageContract, "filter.decision")}
              value={filters.find((filter) => filter.selected)?.key ?? decision}
              items={filters.map((filter) => ({
                value: filter.key,
                label: filter.label,
                count: num(filter.count),
                href: hrefWithQuery(sp, {
                  decision: filter.key === DEFAULT_DECISION ? null : filter.key,
                  ap_cursor: null,
                  ap_status: null,
                  ap_code: null,
                }),
              }))}
            />
          </Box>
        ) : null}

        {/* The decision tabs drive a server navigation. The kit `TabPanel` content transition is
            NOT wrapped around this body: it branches on `useReducedMotion()`, which is false on the
            server and true on a reduced-motion client, so it renders a different element tree on
            each side and hydration fails on every load under that setting. Put it back once the kit
            renders one tree and only zeroes the durations. */}
        {animals.length === 0 ? (
          <EmptyState title={copy(pageContract, decision === DEFAULT_DECISION ? "empty.pending" : "empty.animals")} />
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
            {animals.map((animal) => {
              // The decision block is the same on both card shapes: who decided and when, the form
              // for a pending row behind the backend control, or the backend's reason.
              const decisionBlock =
                animal.decision !== "pending" ? (
                  <div className="small" style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                    <span className="muted">
                      {copy(pageContract, "decision.by")} {animal.decided_by_name || none}
                      {animal.decided_at ? ` ${copy(pageContract, "decision.on")} ${fmtDateTime(animal.decided_at)}` : ""}
                    </span>
                    {animal.decision_note ? <span>{animal.decision_note}</span> : null}
                  </div>
                ) : canDecide ? (
                  <AnimalPurchaseDecisionForm candidateId={animal.candidate_id} rowVersion={animal.row_version} labels={decisionLabels} />
                ) : (
                  // A principal who can open the page but not decide sees the backend's reason,
                  // never a button that would 403.
                  <div className="muted small">{decideDisabledReason || copy(pageContract, "verdict.disabled_no_access")}</div>
                );

              // Backend-owned row title ("Animal 7 · Female goat"), the CEO's decision chip, the
              // buying desk's own field verdict beside it, and the load.
              const heading = (
                <div style={{ display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
                  <b style={{ fontSize: 15 }}>{animal.title}</b>
                  <Tag tone={decisionTone(animal.decision_tone)}>{animal.decision_label}</Tag>
                  <FieldVerdictChip animal={animal} hint={sopCopy.fieldVerdictHint} />
                  <span className="muted small">
                    {animalColumn("load_ref")} {animal.load_ref}
                  </span>
                </div>
              );

              if (animal.questionnaire_version > 0) {
                // A row recorded under the Procurement SOP questionnaire: captures per slot on the
                // left, the answers by section on the right, the decision under the answers.
                return (
                  <article key={animal.candidate_id} className="card ap-animal ap-sop" aria-label={animal.title}>
                    {heading}
                    {/* The captures as one strip in recorded order, the answers beneath in compact
                        columns, the decision as the card's last line. */}
                    <AnimalPurchaseMedia slots={animal.media_slots ?? []} copy={sopCopy} />
                    <AnimalPurchaseAnswers rows={animal.answer_rows ?? []} copy={sopCopy} />
                    <div className="ap-sop-decision">{decisionBlock}</div>
                  </article>
                );
              }

              // A legacy row recorded before the questionnaire: one video and the few facts, in
              // the same card shape as an SOP row (a tile strip, then the facts, then the decision).
              return (
                <article key={animal.candidate_id} className="card ap-animal ap-sop" aria-label={animal.title}>
                  {heading}
                  <div className="ap-sop-media">
                    <div className="ap-tiles">
                      {animal.media_url ? (
                        <AnimalPurchaseLightbox
                          items={[{ proofRef: animal.video_proof_ref || animal.candidate_id, url: animal.media_url, kind: "video", title: copy(pageContract, "video.title") }]}
                          openLabel={sopCopy.photoOpen}
                          closeLabel={sopCopy.close}
                        />
                      ) : (
                        <figure className="ap-tile">
                          <div className="ap-tile-btn empty muted small">{copy(pageContract, "video.empty")}</div>
                          <figcaption className="muted small">{copy(pageContract, "video.title")}</figcaption>
                        </figure>
                      )}
                    </div>
                  </div>
                  <dl className="ap-facts">
                    <div className="ap-sop-row">
                      <dt>{animalColumn("breed")}</dt>
                      <dd>{animal.breed || none}</dd>
                    </div>
                    <div className="ap-sop-row">
                      <dt>{animalColumn("age_months")}</dt>
                      <dd>{animal.age_months == null ? none : num(animal.age_months)}</dd>
                    </div>
                    <div className="ap-sop-row">
                      <dt>{animalColumn("weight_kg")}</dt>
                      <dd>{animal.weight_kg == null ? none : num(animal.weight_kg, 1)}</dd>
                    </div>
                    <div className="ap-sop-row">
                      <dt>{animalColumn("condition")}</dt>
                      <dd>{animal.condition_label || none}</dd>
                    </div>
                    <div className="ap-sop-row">
                      <dt>{animalColumn("temp_tag")}</dt>
                      <dd>{animal.temp_tag || none}</dd>
                    </div>
                    <div className="ap-sop-row">
                      <dt>{animalColumn("notes")}</dt>
                      <dd>{animal.notes || none}</dd>
                    </div>
                  </dl>
                  <div className="ap-sop-decision">{decisionBlock}</div>
                </article>
              );
            })}
          </div>
        )}

        {animalsNextCursor || animalCursor ? (
          <ProcurementTableFooter
            denseLabel={copy(pageContract, "action.dense", "Dense")}
            rowsLabel={copy(pageContract, "pager.rows", "Rows")}
            page={1}
            pageCount={1}
            prevHref={animalCursor ? hrefWithQuery(sp, { ap_cursor: null, ap_status: null, ap_code: null }) : null}
            nextHref={animalsNextCursor ? hrefWithQuery(sp, { ap_cursor: animalsNextCursor, ap_status: null, ap_code: null }) : null}
            rangeLabel={`${num(animals.length)} ${copy(pageContract, animals.length === 1 ? "pager.noun.one" : "pager.noun")}`}
            prevLabel={copy(pageContract, "action.prev_page")}
            nextLabel={copy(pageContract, "action.next_page")}
          />
        ) : null}
      </section>
    </div>
  );
}

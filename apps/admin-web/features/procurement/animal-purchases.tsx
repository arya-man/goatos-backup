import type { ReactNode } from "react";
import Link from "@/components/no-prefetch-link";
import { redirect } from "next/navigation";
import { Truck, Video } from "lucide-react";
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
  const loadsLimit = loadsTable.page_size_options[0] ?? DEFAULT_LIMIT;
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

  const loads: AnimalPurchaseLoad[] = loadsResult.ok ? loadsResult.data.loads : [];
  const loadsNextCursor = loadsResult.ok ? loadsResult.data.next_cursor : undefined;
  const animals: AnimalPurchaseAnimal[] = reviewResult.ok ? reviewResult.data.animals : [];
  const animalsNextCursor = reviewResult.ok ? reviewResult.data.next_cursor : undefined;
  const filters = reviewResult.ok ? reviewResult.data.filters : [];
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

      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="crumb">
            <b>{copy(pageContract, "crumb")}</b> · {pageContract.title}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
      </div>

      {/* Decision feedback from the Server Action's redirect. Every code resolves to page copy;
          an unknown one falls back to the generic failure line rather than leaking the token. */}
      {feedback.status ? (
        <div className={feedback.status === "success" ? "note" : "alert"} style={{ marginBottom: 14 }} role="status">
          {feedbackText}
        </div>
      ) : null}

      {!loadsResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{loadsResult.error.code ?? loadsResult.error.kind}</b>&nbsp;{loadsResult.error.message}
        </div>
      ) : null}
      {!reviewResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{reviewResult.error.code ?? reviewResult.error.kind}</b>&nbsp;{reviewResult.error.message}
        </div>
      ) : null}

      {/* Whole-desk figures from the backend counts, never sums over the rendered page. */}
      <div className="grid g4 kpi-row" style={{ marginBottom: 14 }}>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "summary.loads")}</div>
          {/* The loads read is one keyset page; a trailing "+" says there are more than shown. */}
          <div className="val">{loadsResult.ok ? `${num(loads.length)}${loadsNextCursor ? "+" : ""}` : none}</div>
          <div className="dl">{loadsTable.title}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "summary.pending")}</div>
          <div className="val">{totals ? num(totals.pending) : none}</div>
          <div className="dl">{copy(pageContract, "summary.hint")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "summary.accepted")}</div>
          <div className="val">{totals ? num(totals.accepted) : none}</div>
          <div className="dl">{copy(pageContract, "summary.hint")}</div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "summary.rejected")}</div>
          <div className="val">{totals ? num(totals.rejected) : none}</div>
          <div className="dl">{copy(pageContract, "summary.hint")}</div>
        </div>
      </div>

      <section className="card" style={{ marginBottom: 14 }}>
        <div className="hd">
          <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.loads.title")}</h3>
          <div className="sp" style={{ flex: 1 }} />
          {/* The load FILTER: a plain query-param link, not an overlay. Clicking a load narrows
              the animals section below; the all-loads chip clears it. */}
          <div className="chips" role="group" aria-label={copy(pageContract, "filter.load")}>
            <span className="muted small" style={{ marginRight: 6 }}>
              {copy(pageContract, "filter.load")}
            </span>
            <Link
              href={hrefWithQuery(sp, { load_id: null, ap_cursor: null, ap_status: null, ap_code: null })}
              scroll={false}
              className={loadId ? "btn sm" : "btn sm p"}
              aria-current={loadId ? undefined : "true"}
            >
              {copy(pageContract, "filter.load.all")}
            </Link>
            {loadId ? (
              <span className="btn sm p" aria-current="true">
                {selectedLoadRef ?? none}
              </span>
            ) : null}
          </div>
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
          <div className="empty">{copy(pageContract, "empty.loads")}</div>
        ) : (
          <div className="twrap" tabIndex={0} role="region" aria-label={loadsTable.title}>
            <table className="animal-purchase-loads-table" aria-label={loadsTable.title}>
              <thead>
                {/* Header labels come from the page contract IN ITS ORDER; the body cells below
                    are written in that same order (load_ref, vendor_name, farm, expected_count,
                    total, pending, accepted, rejected, created_at). */}
                <tr>
                  {loadColumns.map((label) => (
                    <th key={label}>{label}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
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
                    <tr key={load.load_id} className={selected ? "on" : undefined} aria-selected={selected ? "true" : undefined}>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(<b>{load.load_ref}</b>)}</td>
                      <td>{cellLink(load.vendor_name || none)}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(load.farm)}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.expected_count))}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.counts.total))}</td>
                      <td style={{ whiteSpace: "nowrap" }}>
                        {cellLink(<Tag tone={load.counts.pending > 0 ? "warn" : "mut"}>{num(load.counts.pending)}</Tag>)}
                      </td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.counts.accepted))}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(num(load.counts.rejected))}</td>
                      <td style={{ whiteSpace: "nowrap" }}>{cellLink(fmtDate(load.created_at))}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}

        {loadsNextCursor || loadCursor ? (
          <div className="pager2">
            <div className="sp" style={{ flex: 1 }} />
            {/* Keyset cursors read forward only; "Back" returns to the first page rather than
                growing a trail — the loads list is expected to stay shallow. */}
            {loadCursor ? (
              <Link href={hrefWithQuery(sp, { ld_cursor: null })} className="btn" scroll={false}>
                {copy(pageContract, "action.prev_page")}
              </Link>
            ) : null}
            {loadsNextCursor ? (
              <Link href={hrefWithQuery(sp, { ld_cursor: loadsNextCursor })} className="btn" scroll={false}>
                {copy(pageContract, "action.next_page")}
              </Link>
            ) : null}
          </div>
        ) : null}
      </section>

      <section className="card">
        <div className="hd">
          <Video className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.animals.title")}</h3>
          <span className="muted small">{copy(pageContract, "section.animals.hint")}</span>
        </div>

        {/* Load and recorded-on window: a plain GET form, so the filter lives in the URL like
            every other list filter and the whole-filter chip counts follow it. The chip and the
            page cursor are dropped on submit by construction (they are not form fields). */}
        <form method="get" action={PATHNAME} className="ap-filter-bar" role="search" aria-label={copy(pageContract, "filter.load")}>
          {decision !== DEFAULT_DECISION ? <input type="hidden" name="decision" value={decision} /> : null}
          <label className="ap-filter">
            <span className="muted small">{copy(pageContract, "filter.load")}</span>
            <select name="load_id" defaultValue={loadId ?? ""} className="ap-filter-select">
              <option value="">{copy(pageContract, "filter.load.all")}</option>
              {loads.map((load) => (
                <option key={load.load_id} value={load.load_id}>
                  {load.load_ref} · {load.vendor_name}
                </option>
              ))}
            </select>
          </label>
          <label className="ap-filter">
            <span className="muted small">{copy(pageContract, "filter.recorded_from")}</span>
            <input type="date" name="recorded_from" defaultValue={recordedFrom} className="ap-filter-date" />
          </label>
          <label className="ap-filter">
            <span className="muted small">{copy(pageContract, "filter.recorded_to")}</span>
            <input type="date" name="recorded_to" defaultValue={recordedTo} className="ap-filter-date" />
          </label>
          <button type="submit" className="btn sm p">
            {copy(pageContract, "filter.apply")}
          </button>
          {loadId || recordedFrom || recordedTo ? (
            <Link href={hrefWithQuery(sp, { load_id: null, recorded_from: null, recorded_to: null, ap_cursor: null, ap_status: null, ap_code: null })} className="btn sm" scroll={false}>
              {copy(pageContract, "filter.clear")}
            </Link>
          ) : null}
        </form>

        {/* Decision chips are the response's own filters: label and WHOLE-FILTER count verbatim,
            selection as the backend reports it. A filter switch drops the cursor by construction. */}
        {filters.length > 0 ? (
          <div className="chips ap-decision-chips" role="group" aria-label={copy(pageContract, "filter.decision")}>
            <span className="muted small" style={{ marginRight: 6 }}>
              {copy(pageContract, "filter.decision")}
            </span>
            {filters.map((filter) => (
              <Link
                key={filter.key}
                href={hrefWithQuery(sp, {
                  decision: filter.key === DEFAULT_DECISION ? null : filter.key,
                  ap_cursor: null,
                  ap_status: null,
                  ap_code: null,
                })}
                scroll={false}
                className={filter.selected ? "btn sm p" : "btn sm"}
                aria-current={filter.selected ? "true" : undefined}
              >
                {filter.label} <span className="chip count">{num(filter.count)}</span>
              </Link>
            ))}
          </div>
        ) : null}

        {animals.length === 0 ? (
          <div className="empty">
            {copy(pageContract, decision === DEFAULT_DECISION ? "empty.pending" : "empty.animals")}
          </div>
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
          <div className="pager2">
            <span className="muted small">
              {num(animals.length)} {copy(pageContract, animals.length === 1 ? "pager.noun.one" : "pager.noun")}
            </span>
            <div className="sp" style={{ flex: 1 }} />
            {animalCursor ? (
              <Link href={hrefWithQuery(sp, { ap_cursor: null, ap_status: null, ap_code: null })} className="btn" scroll={false}>
                {copy(pageContract, "action.prev_page")}
              </Link>
            ) : null}
            {animalsNextCursor ? (
              <Link href={hrefWithQuery(sp, { ap_cursor: animalsNextCursor, ap_status: null, ap_code: null })} className="btn" scroll={false}>
                {copy(pageContract, "action.next_page")}
              </Link>
            ) : null}
          </div>
        ) : null}
      </section>
    </div>
  );
}

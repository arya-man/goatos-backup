import { redirect } from "next/navigation";
import { AlertTriangle, Search } from "lucide-react";

import { copy, optionalCopy, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { controlEnabled, control } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getHealthConfigProtocol,
  listHealthConfigMedicines,
  listHealthConfigProtocols,
  type ApiResult,
  type HealthCatalogItem,
  type HealthConfigProtocolDetail,
  type HealthConfigProtocolRow,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import type { RouteSearchParams } from "@/lib/search-params";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { createDisease, discardDraft, openDraft, publishDraft, saveDraft } from "./health-config-actions";
import { HealthRegisterSection } from "./health-register";
import { AddDiseaseForm, BackToListButton, DraftEditor, ProtocolActionButton } from "./health-config-editor";

// Health -> Health Config. The authored treatment rulebook a diagnosis loads from: per disease, per
// age band, the day-by-day course of medicines, actions and critical handoffs.
//
// EDITS ARE VERSIONED, NEVER IN PLACE. Saving builds a DRAFT; publishing promotes it and retires
// the version it replaces. `health_cases` pins the version each goat was diagnosed under, so an
// animal mid-treatment finishes on the dosages it started on and the version it was actually
// treated from stays readable forever. The UI therefore always shows the LIVE version and the OPEN
// DRAFT side by side rather than one "current" protocol — an author needs to see that the live
// course still says 5 ml while their unpublished draft says 3 ml.
//
// ADULT AND KID ARE SEPARATE ROWS. They are separately authored documents that a diagnosis picks
// between using the goat's own age band. Two of the 27 imported diseases genuinely differ, and
// collapsing them into one disease row would hide exactly that.
//
// NO KPI CARDS. The catalog endpoint returns a keyset cursor and no total, because counting the
// filtered rulebook on each request is compute-on-read. A "N protocols" headline computed from the
// visible page would be a false statement about the rulebook.

const PAGE_PATH = "/health/config";
// The catalog is authored config (54 rows today), not herd data. 25 keeps it a bounded page while
// showing a whole disease's two bands together in almost every case.
const CATALOG_PAGE_SIZE = 25;

function SectionError({
  result,
  pageContract,
}: {
  result: ApiResult<unknown> | null;
  pageContract: AdminUiPageContract;
}) {
  if (!result || result.ok) return null;
  return (
    <div className="alert" style={{ marginBottom: 16 }}>
      <AlertTriangle className="ic" aria-hidden="true" />
      <div>
        <b>{copy(pageContract, "action.error_backend")}</b>
        <div className="small muted">
          {result.error.code ?? result.error.kind}&nbsp;{result.error.message}
        </div>
      </div>
    </div>
  );
}

/**
 * The live-version cell.
 *
 * A protocol with no published version is a REAL state, not a loading one: a disease that was
 * created and drafted but never published has no live course, and a diagnosis for it would fail.
 * It gets its own visible label rather than an empty cell.
 */
function LiveVersion({
  row,
  pageContract,
}: {
  row: HealthConfigProtocolRow;
  pageContract: AdminUiPageContract;
}) {
  if (!row.published_version_id) {
    return <span className="tag t-warn">{copy(pageContract, "status.no_live")}</span>;
  }
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 6, whiteSpace: "nowrap" }}>
      <span className="tag t-ok">{copy(pageContract, "status.live")}</span>
      <span className="muted" style={{ fontVariantNumeric: "tabular-nums" }}>
        v{row.published_version}
      </span>
    </span>
  );
}

/**
 * The two halves of the rulebook.
 *
 * Plain links, not client state: the tab is part of the URL so a vet can send "the diagnosis
 * register for kids on milk" to someone and have it open there. Switching tabs also drops the
 * other tab's selection, or a stale ?hc_version= would reopen an editor the author has left.
 */
function RulebookTabs({
  tab,
  pageContract,
  basePath,
  searchParams,
}: {
  tab: "treatment" | "diagnosis";
  pageContract: AdminUiPageContract;
  basePath: string;
  searchParams: RouteSearchParams;
}) {
  const href = (next: "treatment" | "diagnosis") => {
    const params = paramsWithout(searchParams, ["hc_tab", "hc_version", "hc_register", "hc_cursor"]);
    if (next === "diagnosis") params.set("hc_tab", "diagnosis");
    const qs = params.toString();
    return qs ? `${basePath}?${qs}` : basePath;
  };
  return (
    <div className="wftoolbar" style={{ gap: 8, marginBottom: 12 }}>
      <a className={tab === "treatment" ? "btn" : "btn ghost"} href={href("treatment")}>
        {copy(pageContract, "tab.protocols")}
      </a>
      <a className={tab === "diagnosis" ? "btn" : "btn ghost"} href={href("diagnosis")}>
        {copy(pageContract, "tab.registers")}
      </a>
    </div>
  );
}

export async function HealthConfigPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const ageBandFilter = (sp.hc_band as string | undefined) || "";
  const searchFilter = (sp.hc_q as string | undefined) || "";
  const draftOnly = (sp.hc_draft as string | undefined) === "only";
  const cursor = (sp.hc_cursor as string | undefined) || "";
  const selectedVersionId = (sp.hc_version as string | undefined) || "";
  // The two halves of the rulebook are two tabs of ONE page: which illness the animal is judged to
  // have, and what it is then given. A second route would let them drift apart in the navigation
  // as well as in the data.
  const tab = (sp.hc_tab as string | undefined) === "diagnosis" ? "diagnosis" : "treatment";
  const selectedRegisterId = (sp.hc_register as string | undefined) || "";

  // The catalog and editor are separate route states. List mode reads exactly one bounded keyset
  // page. Editor mode reads exactly one selected version. Do not fetch the catalog behind the
  // full-screen editor: that turns a simple edit open into unnecessary backend fanout and regresses
  // the latency of the click Ravi is trying to make feel direct.
  const catalogResult = selectedVersionId || tab === "diagnosis"
    ? null
    : await listHealthConfigProtocols({
        age_band: ageBandFilter === "adult" || ageBandFilter === "kid" ? ageBandFilter : undefined,
        search: searchFilter || undefined,
        draft_only: draftOnly || undefined,
        cursor: cursor || undefined,
        limit: CATALOG_PAGE_SIZE,
      });
  const detailResult =
    selectedVersionId && tab !== "diagnosis" ? await getHealthConfigProtocol(selectedVersionId) : null;
  // The medicine picker's source. Fetched only with the editor: the catalog is of no use to
  // the list, and reading it there would be one backend call per page view for nothing.
  const medicinesResult = detailResult ? await listHealthConfigMedicines() : null;

  const authError = firstAuthRequiredError(catalogResult, detailResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const catalog = catalogResult?.ok ? catalogResult.data : null;
  const detail: HealthConfigProtocolDetail | null =
    detailResult && detailResult.ok ? detailResult.data : null;
  // A selected version that no longer resolves. This is a REAL state, not an edge case: the author
  // discards a draft, or a second tab publishes it, and this tab is left holding a ?hc_version=
  // that points at nothing. The server owns the recovery so it works on a cold load of that URL
  // too -- the dead section is simply not rendered, and the notice below says why. Any OTHER
  // failure (a 500, the backend down) still surfaces as an error band rather than being read as
  // "this version is gone".
  const selectedVersionIsGone =
    Boolean(selectedVersionId) && detailResult !== null && !detailResult.ok && detailResult.error.kind === "not_found";

  const mayWrite = controlEnabled(pageContract, "add_disease", true);
  const writeDisabledReason = control(pageContract, "add_disease").disabled_reason || "";

  const catalogCols = tableLabels(pageContract, "protocol-catalog");
  const rows = catalog?.items ?? [];
  const hasFilter = Boolean(ageBandFilter || searchFilter || draftOnly);

  const filterFields: WorklistFilterField[] = [
    {
      kind: "select",
      param: "hc_band",
      label: copy(pageContract, "filter.age_band_label"),
      value: ageBandFilter,
      options: [
        { value: "adult", label: copy(pageContract, "label.age_band.adult") },
        { value: "kid", label: copy(pageContract, "label.age_band.kid") },
      ],
    },
    {
      kind: "select",
      param: "hc_draft",
      label: copy(pageContract, "filter.draft_label"),
      value: draftOnly ? "only" : "",
      options: [{ value: "only", label: copy(pageContract, "filter.draft_only") }],
    },
  ];

  // Keyset paging: "Next" carries the backend's cursor; there is no page number and no total,
  // because both would require counting the filtered rulebook on every request.
  const nextParams = new URLSearchParams();
  if (ageBandFilter) nextParams.set("hc_band", ageBandFilter);
  if (searchFilter) nextParams.set("hc_q", searchFilter);
  if (draftOnly) nextParams.set("hc_draft", "only");
  const restartHref = nextParams.toString() ? `${PAGE_PATH}?${nextParams.toString()}` : PAGE_PATH;
  if (catalog?.next_cursor) nextParams.set("hc_cursor", catalog.next_cursor);
  const nextHref = catalog?.next_cursor ? `${PAGE_PATH}?${nextParams.toString()}` : null;
  const listParams = paramsWithout(sp, ["hc_version"]);
  const listHref = listParams.toString() ? `${PAGE_PATH}?${listParams.toString()}` : PAGE_PATH;

  if (tab === "diagnosis") {
    const registerListHref = (() => {
      const params = paramsWithout(sp, ["hc_register", "hc_version", "hc_cursor"]);
      params.set("hc_tab", "diagnosis");
      return `${PAGE_PATH}?${params.toString()}`;
    })();
    return (
      <div className="screen on">
        <div className="phead">
          <div>
            <div className="crumb">
              {copy(pageContract, "crumb")} / <b>{copy(pageContract, "tab.registers")}</b>
            </div>
            <h1>{pageContract.title}</h1>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          {selectedRegisterId ? (
            <BackToListButton
              href={registerListHref}
              label={optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
            />
          ) : null}
        </div>

        <RulebookTabs tab={tab} pageContract={pageContract} basePath={PAGE_PATH} searchParams={sp} />

        <HealthRegisterSection
          selectedVersionId={selectedRegisterId}
          pageContract={pageContract}
          mayWrite={mayWrite}
          writeDisabledReason={writeDisabledReason}
          listHref={registerListHref}
        />
      </div>
    );
  }

  if (detail) {
    return (
      <div className="screen on">
        <div className="phead" style={{ alignItems: "flex-start", gap: 12 }}>
          <div>
            <div className="crumb">
              {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.catalog.title")}</b>
            </div>
            <h1>
              {detail.display_name} ·{" "}
              {copy(pageContract, detail.age_band === "kid" ? "label.age_band.kid" : "label.age_band.adult")}
            </h1>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <BackToListButton
            href={listHref}
            label={optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
          />
        </div>

        <SelectedProtocolEditor
          detail={detail}
          medicines={medicinesResult?.ok ? medicinesResult.data.medicines : []}
          pageContract={pageContract}
          mayWrite={mayWrite}
          writeDisabledReason={writeDisabledReason}
          listHref={listHref}
        />
      </div>
    );
  }

  if (selectedVersionIsGone) {
    return (
      <div className="screen on">
        <div className="phead" style={{ alignItems: "flex-start", gap: 12 }}>
          <div>
            <div className="crumb">
              {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.catalog.title")}</b>
            </div>
            <h1>{pageContract.title}</h1>
          </div>
          <div className="sp" style={{ flex: 1 }} />
          <BackToListButton
            href={listHref}
            label={optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
          />
        </div>
        <div className="alert" style={{ marginBottom: 16 }}>
          <AlertTriangle className="ic" aria-hidden="true" />
          <div>
            {optionalCopy(pageContract, "error.stale_version") ??
              copy(pageContract, "action.error_backend")}{" "}
            <a href={listHref} style={{ textDecoration: "underline", whiteSpace: "nowrap" }}>
              {optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
            </a>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.catalog.title")}</b>
          </div>
          <h1>{pageContract.title}</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <AddDiseaseForm
          pageContract={pageContract}
          action={createDisease}
          enabled={mayWrite}
          disabledReason={writeDisabledReason}
        />
      </div>

      <RulebookTabs tab={tab} pageContract={pageContract} basePath={PAGE_PATH} searchParams={sp} />

      <SectionError result={catalogResult} pageContract={pageContract} />

      {/* ------------------------------------------------------------------ the protocol catalog */}
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.catalog.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.catalog.caption")}</span>
        </div>
        <p className="small muted" style={{ margin: "0 14px 10px", lineHeight: 1.6 }}>
          {copy(pageContract, "section.catalog.note")}
        </p>

        {/* Disease search. A plain GET form, not a WorklistFilters field: that component has no text
            kind, and the server-rendered form is what the other keyset-paged authority screens
            (Audit Log, DLQ) already use. The match runs in the BACKEND (`search` on
            /health-config/protocols) because the catalog is keyset-paginated — filtering the 20 rows
            of the current page in the browser would silently hide matches sitting on later pages.
            `hc_cursor` is deliberately NOT preserved: a new search restarts paging, or the cursor
            from the old result set would be applied to a different one. */}
        <div className="wftoolbar" style={{ marginBottom: 0 }}>
          <form className="tsearch" action={PAGE_PATH} style={{ maxWidth: 300 }} title={copy(pageContract, "filter.search_label")}>
            {preservedHiddenInputs(sp, ["hc_q", "hc_cursor"])}
            <Search className="ic" style={{ width: 15 }} aria-hidden="true" />
            <input
              name="hc_q"
              defaultValue={searchFilter}
              placeholder={copy(pageContract, "filter.search_label")}
              aria-label={copy(pageContract, "filter.search_label")}
            />
          </form>
        </div>

        <WorklistFilters
          basePath={PAGE_PATH}
          pageParam="hc_cursor"
          fields={filterFields}
          pageContract={pageContract}
        />

        <div
          className="bd health-scroll"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.catalog.aria")}
        >
          <table className="feed-table" aria-label={copy(pageContract, "section.catalog.aria")}>
            <thead>
              <tr>
                {catalogCols.map((col) => (
                  <th key={col}>{col}</th>
                ))}
                <th>{copy(pageContract, "action.edit_protocol")}</th>
              </tr>
            </thead>
            <tbody>
              {rows.length === 0 ? (
                <tr>
                  <td colSpan={catalogCols.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {catalogResult?.ok
                        ? hasFilter
                          ? copy(pageContract, "empty.search")
                          : copy(pageContract, "empty.catalog")
                        : copy(pageContract, "action.error_backend")}
                    </div>
                  </td>
                </tr>
              ) : (
                rows.map((row) => (
                  <tr key={`${row.disease_key}:${row.age_band}`}>
                    <td>{row.display_name}</td>
                    <td className="muted">
                      {copy(pageContract, row.age_band === "kid" ? "label.age_band.kid" : "label.age_band.adult")}
                    </td>
                    <td style={{ fontVariantNumeric: "tabular-nums" }}>{row.duration_days}</td>
                    <td style={{ fontVariantNumeric: "tabular-nums" }}>{row.step_count}</td>
                    <td style={{ fontVariantNumeric: "tabular-nums" }}>{row.medication_count}</td>
                    <td style={{ fontVariantNumeric: "tabular-nums" }}>{row.critical_action_count}</td>
                    <td>
                      <LiveVersion row={row} pageContract={pageContract} />
                    </td>
                    <td>
                      {row.has_draft ? (
                        <span className="tag t-info">{copy(pageContract, "status.draft_open")}</span>
                      ) : (
                        <span className="muted">{copy(pageContract, "status.draft_none")}</span>
                      )}
                    </td>
                    <td>
                      <ProtocolActionButton
                        pageContract={pageContract}
                        action={openDraft}
                        fields={{ disease_key: row.disease_key, age_band: row.age_band }}
                        labelKey="action.edit_protocol"
                        navigateOnSuccess="selected-version"
                        basePath={listHref}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>

        <div className="pager2">
          <span className="small muted" style={{ marginRight: "auto" }}>
            {rows.length} · {copy(pageContract, "pager.rows_note")}
          </span>
          {cursor ? (
            <a className="btn sm" href={restartHref}>
              {copy(pageContract, "pager.restart")}
            </a>
          ) : null}
          {nextHref ? (
            <a className="btn sm" href={nextHref}>
              {copy(pageContract, "pager.next")}
            </a>
          ) : null}
        </div>
      </section>

      {/* ------------------------------------------------------------------- the selected course */}
    </div>
  );
}

function SelectedProtocolEditor({
  detail,
  medicines,
  pageContract,
  mayWrite,
  writeDisabledReason,
  listHref,
}: {
  detail: HealthConfigProtocolDetail;
  /** The farm's active medicines — a step names one of these and nothing else. */
  medicines: HealthCatalogItem[];
  pageContract: AdminUiPageContract;
  mayWrite: boolean;
  writeDisabledReason: string;
  listHref: string;
}) {
  return (
    <section className="card" style={{ marginBottom: 16 }}>
      <div className="hd" style={{ flexWrap: "wrap", alignItems: "flex-start", gap: 8 }}>
        <h3>
          {detail.display_name} ·{" "}
          {copy(pageContract, detail.age_band === "kid" ? "label.age_band.kid" : "label.age_band.adult")}
        </h3>
        <span className="small muted">{copy(pageContract, "section.steps.caption")}</span>
      </div>

      <div className="bd" style={{ display: "flex", flexDirection: "column", gap: 12 }}>
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "center" }}>
          <span className={detail.status === "published" ? "tag t-ok" : detail.status === "draft" ? "tag t-info" : "tag t-mut"}>
            {copy(
              pageContract,
              detail.status === "published"
                ? "status.live"
                : detail.status === "draft"
                  ? "status.draft"
                  : "status.retired",
            )}
          </span>
          <span className="muted small" style={{ fontVariantNumeric: "tabular-nums" }}>
            v{detail.version}
          </span>
          <span className="small muted">
            {copy(pageContract, "label.open_cases")}: {detail.open_case_count}
          </span>
        </div>

        <p className="small muted" style={{ margin: 0, lineHeight: 1.6 }}>
          {copy(pageContract, "note.publish_effect")}
        </p>

        {detail.status === "draft" ? (
          <>
            <DraftEditor
              medicines={medicines}
              pageContract={pageContract}
              draft={detail}
              action={saveDraft}
              enabled={mayWrite}
              disabledReason={writeDisabledReason}
            />
            <div style={{ display: "flex", gap: 8, flexWrap: "wrap", alignItems: "flex-start" }}>
              <ProtocolActionButton
                pageContract={pageContract}
                action={publishDraft}
                fields={{ protocol_version_id: detail.protocol_version_id }}
                labelKey="action.publish_protocol"
                confirmKey="note.publish_effect"
                primary
                enabled={mayWrite}
                disabledReason={writeDisabledReason}
              />
              <ProtocolActionButton
                pageContract={pageContract}
                action={discardDraft}
                fields={{ protocol_version_id: detail.protocol_version_id }}
                labelKey="action.discard_draft"
                confirmKey="section.history.note"
                navigateOnSuccess="base"
                basePath={listHref}
                enabled={mayWrite}
                disabledReason={writeDisabledReason}
              />
            </div>
          </>
        ) : (
          // A published or retired version is read-only, and that is a business rule rather
          // than a permission: goats are being treated from it. Editing goes through a draft.
          <div className="bd" style={{ padding: 0, overflowX: "auto" }}>
            <table className="feed-table" aria-label={copy(pageContract, "section.steps.aria")}>
              <thead>
                <tr>
                  {tableLabels(pageContract, "protocol-steps").map((col) => (
                    <th key={col}>{col}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {(detail.steps ?? []).length === 0 ? (
                  <tr>
                    <td colSpan={9}>
                      <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                        {copy(pageContract, "empty.steps")}
                      </div>
                    </td>
                  </tr>
                ) : (
                  (detail.steps ?? []).map((step) => (
                    <tr key={step.step_id ?? `${step.day_no}-${step.seq}`}>
                      <td style={{ fontVariantNumeric: "tabular-nums" }}>{step.day_no}</td>
                      <td className="muted">
                        {copy(pageContract, `label.session.${step.session === "unscheduled" ? "unscheduled" : step.session}`)}
                      </td>
                      <td>
                        {copy(
                          pageContract,
                          step.record_type === "medication"
                            ? "label.record_type.medicine"
                            : step.record_type === "critical_action"
                              ? "label.record_type.critical"
                              : "label.record_type.action",
                        )}
                      </td>
                      <td>{step.medicine_name ?? ""}</td>
                      <td style={{ fontVariantNumeric: "tabular-nums" }}>{step.dosage_text ?? ""}</td>
                      <td className="muted">{step.dosage_denominator ?? ""}</td>
                      <td>{step.medicine_route ?? ""}</td>
                      <td style={{ whiteSpace: "pre-wrap", minWidth: 260 }}>{step.instruction ?? ""}</td>
                      <td>
                        {step.critical_action_type
                          ? copy(
                              pageContract,
                              step.critical_action_type === "lifecycle_exit"
                                ? "label.critical.exit"
                                : "label.critical.quarantine",
                            )
                          : ""}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        )}

        {(detail.history ?? []).length > 0 ? (
          <div>
            <h4 style={{ margin: "6px 0" }}>{copy(pageContract, "section.history.title")}</h4>
            <p className="small muted" style={{ margin: "0 0 8px", lineHeight: 1.6 }}>
              {copy(pageContract, "section.history.note")}
            </p>
            <ul className="small" style={{ margin: 0, paddingLeft: 18, lineHeight: 1.8 }}>
              {/* Every number here is labelled. A bare "v1 · Draft · 0 · 2" tells a reader
                  nothing about which figure is steps and which is days — and on a screen whose
                  subject is dosages, an unlabelled number is worse than no number. */}
              {(detail.history ?? []).map((version) => (
                <li key={version.protocol_version_id}>
                  v{version.version} ·{" "}
                  {copy(
                    pageContract,
                    version.status === "published"
                      ? "status.live"
                      : version.status === "draft"
                        ? "status.draft"
                        : "status.retired",
                  )}{" "}
                  · {copy(pageContract, "label.step_count")}: {version.step_count} ·{" "}
                  {copy(pageContract, "label.duration_days")}: {version.duration_days}
                  {version.published_at ? ` · ${version.published_at.slice(0, 10)}` : ""}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>
    </section>
  );
}

/**
 * Carries the other live filters through the search form's GET submit.
 *
 * A bare `<form method=GET>` replaces the whole query string with its own fields, so without these
 * hidden inputs searching would silently drop the age-band and draft-state filters the author had
 * applied — the rows would change for two reasons at once and the screen could not explain either.
 */
function preservedHiddenInputs(params: RouteSearchParams, exclude: string[]) {
  const excluded = new Set(exclude);
  return Object.entries(params).flatMap(([key, value]) => {
    if (excluded.has(key)) return [];
    if (Array.isArray(value)) {
      return value.map((item) => <input key={`${key}:${item}`} type="hidden" name={key} value={item} />);
    }
    return value ? [<input key={key} type="hidden" name={key} value={value} />] : [];
  });
}

function paramsWithout(params: RouteSearchParams, exclude: string[]) {
  const excluded = new Set(exclude);
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (excluded.has(key)) continue;
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  return next;
}

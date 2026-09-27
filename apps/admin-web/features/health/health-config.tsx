import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import Link from "@/components/no-prefetch-link";
import Form from "next/form";
import Box from "@mui/material/Box";
import { SearchTextField } from "@/components/app/list/search-text-field";

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
import { HealthTypesSection } from "./health-types";
import { RulebookTabStrip } from "./health-config-tab-strip";
import { StaleVersionNotice } from "./health-stale-version-recovery";
import { AddDiseaseForm, BackToListButton, DraftEditor, ProtocolActionButton } from "./health-config-editor";
import { InfoHint } from "@/components/app/info-hint";
import { PageHeader } from "@/components/app/page-header";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import TableContainer from "@mui/material/TableContainer";
import { visuallyHidden } from "@mui/utils";
import { Label } from "@/components/minimal/label";
import { EmptyContent } from "@/components/minimal/empty-content";
import { LinkButton } from "@/components/app/link-button";
import { CATALOG_PAGE_SIZE } from "./health-config-layout";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";

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

function SectionError({
  result,
  pageContract,
}: {
  result: ApiResult<unknown> | null;
  pageContract: AdminUiPageContract;
}) {
  if (!result || result.ok) return null;
  return (
    <Alert severity="error">
      <AlertTitle>{copy(pageContract, "action.error_backend")}</AlertTitle>
      {result.error.code ?? result.error.kind}&nbsp;{result.error.message}
    </Alert>
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
    return <Label variant="soft" color="warning">{copy(pageContract, "status.no_live")}</Label>;
  }
  return (
    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, whiteSpace: "nowrap" }}>
      <Label variant="soft" color="success">{copy(pageContract, "status.live")}</Label>
      <Box component="span" sx={{ color: "text.secondary", fontVariantNumeric: "tabular-nums" }}>
        v{row.published_version}
      </Box>
    </Box>
  );
}

/**
 * The two halves of the rulebook.
 *
 * Links, not client state: the tab is part of the URL so a vet can send "the diagnosis register
 * for kids on milk" to someone and have it open there. Switching tabs also drops the other tab's
 * selection, or a stale ?hc_version= would reopen an editor the author has left.
 *
 * They are the console's own Link, never a bare <a>. The two tabs read DIFFERENT server data --
 * Treatment lists the protocol catalog, Diagnosis the registers -- so this is a data-changing tab
 * and it navigates, which `docs/decisions/admin-web-interaction-patterns.md` rule 3 allows. What
 * it must not do is reload the DOCUMENT: a bare <a> tore down the shell, the sidebar and every
 * bit of client state to swap one panel, which is what "the whole page is loading" looked like.
 * A Link makes it an RSC transition of this segment alone; the shell stays mounted.
 */
function RulebookTabs({
  tab,
  pageContract,
  basePath,
  searchParams,
}: {
  tab: "treatment" | "diagnosis" | "types";
  pageContract: AdminUiPageContract;
  basePath: string;
  searchParams: RouteSearchParams;
}) {
  const href = (next: "treatment" | "diagnosis" | "types") => {
    const params = paramsWithout(searchParams, ["hc_tab", "hc_version", "hc_register", "hc_cursor"]);
    if (next !== "treatment") params.set("hc_tab", next);
    const qs = params.toString();
    return qs ? `${basePath}?${qs}` : basePath;
  };
  return (
    <RulebookTabStrip activeHref={href(tab)}>
      <Link href={href("treatment")}>
        {copy(pageContract, "tab.protocols")}
      </Link>
      <Link href={href("diagnosis")}>
        {copy(pageContract, "tab.registers")}
      </Link>
      <Link href={href("types")}>
        {copy(pageContract, "tab.types")}
      </Link>
    </RulebookTabStrip>
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
  const rawTab = (sp.hc_tab as string | undefined) ?? "";
  const tab: "treatment" | "diagnosis" | "types" =
    rawTab === "diagnosis" ? "diagnosis" : rawTab === "types" ? "types" : "treatment";
  const selectedRegisterId = (sp.hc_register as string | undefined) || "";

  // The catalog and editor are separate route states. List mode reads exactly one bounded keyset
  // page. Editor mode reads exactly one selected version. Do not fetch the catalog behind the
  // full-screen editor: that turns a simple edit open into unnecessary backend fanout and regresses
  // the latency of the click Ravi is trying to make feel direct.
  const catalogResult = selectedVersionId || tab !== "treatment"
    ? null
    : await listHealthConfigProtocols({
        age_band: ageBandFilter === "adult" || ageBandFilter === "kid" ? ageBandFilter : undefined,
        search: searchFilter || undefined,
        draft_only: draftOnly || undefined,
        cursor: cursor || undefined,
        limit: CATALOG_PAGE_SIZE,
      });
  // The editor's two reads go together, not one after the other.
  //
  // The medicine picker's source is still fetched ONLY with the editor -- the catalog is of no use
  // to the list, and reading it there would be one backend call per page view for nothing. What
  // changed is that it no longer WAITS for the version: `detailResult ? await …` read as a data
  // dependency, but it is only a GATE, and the condition that opens it is known before either call.
  // Serialising them put one whole round trip between the author's click and the editor for no
  // reason, and `check-serial-await` cannot see it -- the second line mentions the first binding,
  // which the guard treats as a genuine dependency.
  const opensProtocolEditor = Boolean(selectedVersionId) && tab === "treatment";
  const [detailResult, medicinesResult] = opensProtocolEditor
    ? await Promise.all([getHealthConfigProtocol(selectedVersionId), listHealthConfigMedicines()])
    : [null, null];

  const authError = firstAuthRequiredError(catalogResult, detailResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

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


  // A dead version recovers TO THE LIST, not to a dead end.
  //
  // This branch used to return the notice ALONE. Because the catalog is deliberately not fetched
  // behind an open editor, "everything below is up to date" sat over an empty screen with a single
  // link to press -- and the commonest way to reach it is the ordinary one: PUBLISHING retires the
  // draft id the editor URL is holding. So the author finished a normal edit and landed on a page
  // that looked broken.
  //
  // The list is read HERE, in the same render, so the recovery works on a cold load of that URL and
  // costs nothing on every other request. It is one extra read on a rare path, which is the right
  // trade against a screen with nothing on it.
  const recoveryCatalogResult = selectedVersionIsGone && tab === "treatment"
    ? await listHealthConfigProtocols({
        age_band: ageBandFilter === "adult" || ageBandFilter === "kid" ? ageBandFilter : undefined,
        search: searchFilter || undefined,
        draft_only: draftOnly || undefined,
        limit: CATALOG_PAGE_SIZE,
      })
    : null;
  const effectiveCatalogResult = catalogResult ?? recoveryCatalogResult;
  const catalog = effectiveCatalogResult?.ok ? effectiveCatalogResult.data : null;

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

  if (tab === "types") {
    return (
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
        <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "tab.types") }]} />

        <RulebookTabs tab={tab} pageContract={pageContract} basePath={PAGE_PATH} searchParams={sp} />

        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={RULEBOOK_SKELETON.types} fallbackBy={RULEBOOK_FALLBACK_BY}>
          <HealthTypesSection
            pageContract={pageContract}
            mayWrite={mayWrite}
            writeDisabledReason={writeDisabledReason}
          />
        </UrlSuspense>
      </Stack>
    );
  }

  if (tab === "diagnosis") {
    const registerListHref = (() => {
      const params = paramsWithout(sp, ["hc_register", "hc_version", "hc_cursor"]);
      params.set("hc_tab", "diagnosis");
      return `${PAGE_PATH}?${params.toString()}`;
    })();
    return (
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "tab.registers") }]}
          actions={
            selectedRegisterId ? (
              <BackToListButton
                href={registerListHref}
                label={optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
              />
            ) : null
          }
        />

        <RulebookTabs tab={tab} pageContract={pageContract} basePath={PAGE_PATH} searchParams={sp} />

        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={RULEBOOK_SKELETON.diagnosis} fallbackBy={RULEBOOK_FALLBACK_BY}>
          <HealthRegisterSection
            selectedVersionId={selectedRegisterId}
            pageContract={pageContract}
            mayWrite={mayWrite}
            writeDisabledReason={writeDisabledReason}
            listHref={registerListHref}
          />
        </UrlSuspense>
      </Stack>
    );
  }

  if (detail) {
    return (
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
        {/* One <h1> per page (spec 1): the section rides in the breadcrumb, the protocol name is the title. */}
        <PageHeader
          title={`${detail.display_name} · ${copy(pageContract, detail.age_band === "kid" ? "label.age_band.kid" : "label.age_band.adult")}`}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }, { label: copy(pageContract, "section.catalog.title") }]}
          actions={
            <BackToListButton
              href={listHref}
              label={optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
            />
          }
        />

        <SelectedProtocolEditor
          detail={detail}
          medicines={medicinesResult?.ok ? listOrEmpty(medicinesResult.data.medicines) : []}
          pageContract={pageContract}
          mayWrite={mayWrite}
          writeDisabledReason={writeDisabledReason}
          listHref={listHref}
        />
      </Stack>
    );
  }

  return (
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <PageHeader
        title={pageContract.title}
        crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "section.catalog.title") }]}
        actions={<AddDiseaseForm pageContract={pageContract} action={createDisease} enabled={mayWrite} disabledReason={writeDisabledReason} />}
      />

      <RulebookTabs tab={tab} pageContract={pageContract} basePath={PAGE_PATH} searchParams={sp} />

      {/* The catalog (guard: url-keyed-panel): a tab / filter / search / page click swaps it to its
          skeleton at once; header and tabs stay on screen. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={RULEBOOK_SKELETON[""]} fallbackBy={RULEBOOK_FALLBACK_BY}>
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      {selectedVersionIsGone ? (
        <StaleVersionNotice
          message={
            optionalCopy(pageContract, "error.stale_version") ?? copy(pageContract, "action.error_backend")
          }
          linkLabel={optionalCopy(pageContract, "action.back_to_list") ?? copy(pageContract, "action.back")}
          listHref={listHref}
        />
      ) : null}

      <SectionError result={effectiveCatalogResult} pageContract={pageContract} />

      {/* ------------------------------------------------------------------ the protocol catalog */}
      {/* Template user-list anatomy: Card, CardHeader, toolbar (search + filters), table, footer. */}
      <Card>
        <CardHeader title={copy(pageContract, "section.catalog.title")} />

        {/* Disease search. A plain GET form, not a WorklistFilters field: that component has no text
            kind, and the server-rendered form is what the other keyset-paged authority screens
            (Audit Log, DLQ) already use. The match runs in the BACKEND (`search` on
            /health-config/protocols) because the catalog is keyset-paginated — filtering the 20 rows
            of the current page in the browser would silently hide matches sitting on later pages.
            `hc_cursor` is deliberately NOT preserved: a new search restarts paging, or the cursor
            from the old result set would be applied to a different one. */}
        {/* Template toolbar search (UserTableToolbar TextField + magnifier), as a GET form. */}
        <Box sx={{ p: 2.5, pb: 0 }}>
          <Form action={PAGE_PATH} scroll={false} title={copy(pageContract, "filter.search_label")}>
            {preservedHiddenInputs(sp, ["hc_q", "hc_cursor"])}
            <SearchTextField name="hc_q" defaultValue={searchFilter} placeholder={copy(pageContract, "filter.search_label")} />
          </Form>
        </Box>

        <WorklistFilters
          basePath={PAGE_PATH}
          pageParam="hc_cursor"
          fields={filterFields}
          pageContract={pageContract}
        />

        <TableContainer
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.catalog.aria")}
        >
          <Table sx={{ minWidth: 960 }} aria-label={copy(pageContract, "section.catalog.aria")}>
            <TableHead>
              <TableRow>
                {catalogCols.map((col) => (
                  <TableCell component="th" key={col}>{col}</TableCell>
                ))}
                <TableCell component="th" align="right" sx={{ width: 88 }}><Box component="span" sx={visuallyHidden}>{copy(pageContract, "action.edit_protocol")}</Box></TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={catalogCols.length + 1} sx={{ p: 0 }}>
                    <EmptyContent
                      sx={{ py: 5 }}
                      title={
                        catalogResult?.ok
                          ? hasFilter
                            ? copy(pageContract, "empty.search")
                            : copy(pageContract, "empty.catalog")
                          : copy(pageContract, "action.error_backend")
                      }
                    />
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row) => (
                  <TableRow key={`${row.disease_key}:${row.age_band}`}>
                    <TableCell>{row.display_name}</TableCell>
                    <TableCell sx={{ color: "text.secondary" }}>
                      {copy(pageContract, row.age_band === "kid" ? "label.age_band.kid" : "label.age_band.adult")}
                    </TableCell>
                    <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{row.duration_days}</TableCell>
                    <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{row.step_count}</TableCell>
                    <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{row.medication_count}</TableCell>
                    <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{row.critical_action_count}</TableCell>
                    <TableCell>
                      <LiveVersion row={row} pageContract={pageContract} />
                    </TableCell>
                    <TableCell>
                      {row.has_draft ? (
                        <Label variant="soft" color="info">{copy(pageContract, "status.draft_open")}</Label>
                      ) : (
                        <Box component="span" sx={{ color: "text.secondary" }}>{copy(pageContract, "status.draft_none")}</Box>
                      )}
                    </TableCell>
                    <TableCell align="right">
                      <ProtocolActionButton
                        pageContract={pageContract}
                        action={openDraft}
                        fields={{ disease_key: row.disease_key, age_band: row.age_band }}
                        labelKey="action.edit_protocol"
                        icon="edit"
                        navigateOnSuccess="selected-version"
                        basePath={listHref}
                        enabled={mayWrite}
                        disabledReason={writeDisabledReason}
                      />
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>

        {/* Template table footer: row count left, keyset Restart / Next right. */}
        <Stack direction="row" sx={{ px: 2.5, py: 1.5, gap: 1, alignItems: "center", borderTop: 1, borderColor: "divider" }}>
          <Typography variant="body2" sx={{ mr: "auto", display: "inline-flex", alignItems: "center", gap: 0.75, color: "text.secondary" }}>
            {rows.length} {copy(pageContract, "pager.rows").toLowerCase()}
            <InfoHint text={copy(pageContract, "pager.rows_note")} />
          </Typography>
          {cursor ? (
            <LinkButton href={restartHref} variant="outlined" size="small">
              {copy(pageContract, "pager.restart")}
            </LinkButton>
          ) : null}
          {nextHref ? (
            <LinkButton href={nextHref} variant="outlined" size="small">
              {copy(pageContract, "pager.next")}
            </LinkButton>
          ) : null}
        </Stack>
      </Card>
      </Stack>
      </UrlSuspense>

      {/* ------------------------------------------------------------------- the selected course */}
    </Stack>
  );
}

/** Each rulebook tab's panel skeleton ("" = Treatment, the default tab). */
const RULEBOOK_SKELETON = {
  "": <PanelSkeleton table={10} tableWidths={Array.from({ length: 9 }, () => "1fr")} />,
  diagnosis: <PanelSkeleton table={10} tableWidths={Array.from({ length: 6 }, () => "1fr")} />,
  types: <PanelSkeleton table={8} tableWidths={Array.from({ length: 4 }, () => "1fr")} />,
};
const RULEBOOK_FALLBACK_BY = { param: "hc_tab", shapes: RULEBOOK_SKELETON };

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
    // Template order-details anatomy: Card, CardHeader, content Stack, item table, history list.
    <Card>
      <CardHeader
        title={`${detail.display_name} · ${copy(pageContract, detail.age_band === "kid" ? "label.age_band.kid" : "label.age_band.adult")}`}
        subheader={copy(pageContract, "section.steps.caption")}
      />

      <Stack spacing={2} sx={{ p: 3 }}>
        <Stack direction="row" sx={{ gap: 1.25, flexWrap: "wrap", alignItems: "center" }}>
          <Label variant="soft" color={detail.status === "published" ? "success" : detail.status === "draft" ? "info" : "default"}>
            {copy(
              pageContract,
              detail.status === "published"
                ? "status.live"
                : detail.status === "draft"
                  ? "status.draft"
                  : "status.retired",
            )}
          </Label>
          <Typography variant="body2" sx={{ color: "text.secondary", fontVariantNumeric: "tabular-nums" }}>
            v{detail.version}
          </Typography>
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "label.open_cases")}: {detail.open_case_count}
          </Typography>
        </Stack>

        <Typography variant="body2" sx={{ color: "text.secondary" }}>
          {copy(pageContract, "note.publish_effect")}
        </Typography>

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
            <Stack direction="row" sx={{ gap: 1, flexWrap: "wrap", alignItems: "flex-start" }}>
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
            </Stack>
          </>
        ) : (
          // A published or retired version is read-only, and that is a business rule rather
          // than a permission: goats are being treated from it. Editing goes through a draft.
          <TableContainer sx={{ mx: -3, width: "auto" }}>
            <Table sx={{ minWidth: 1080 }} aria-label={copy(pageContract, "section.steps.aria")}>
              <TableHead>
                <TableRow>
                  {tableLabels(pageContract, "protocol-steps").map((col) => (
                    <TableCell component="th" key={col}>{col}</TableCell>
                  ))}
                </TableRow>
              </TableHead>
              <TableBody>
                {(detail.steps ?? []).length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} sx={{ p: 0 }}>
                      <EmptyContent sx={{ py: 5 }} title={copy(pageContract, "empty.steps")} />
                    </TableCell>
                  </TableRow>
                ) : (
                  (detail.steps ?? []).map((step) => (
                    <TableRow key={step.step_id ?? `${step.day_no}-${step.seq}`}>
                      <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{step.day_no}</TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>
                        {copy(pageContract, `label.session.${step.session === "unscheduled" ? "unscheduled" : step.session}`)}
                      </TableCell>
                      <TableCell>
                        {copy(
                          pageContract,
                          step.record_type === "medication"
                            ? "label.record_type.medicine"
                            : step.record_type === "critical_action"
                              ? "label.record_type.critical"
                              : "label.record_type.action",
                        )}
                      </TableCell>
                      <TableCell>{step.medicine_name ?? ""}</TableCell>
                      <TableCell sx={{ fontVariantNumeric: "tabular-nums" }}>{step.dosage_text ?? ""}</TableCell>
                      <TableCell sx={{ color: "text.secondary" }}>{step.dosage_denominator ?? ""}</TableCell>
                      <TableCell>{step.medicine_route ?? ""}</TableCell>
                      <TableCell sx={{ whiteSpace: "pre-wrap", minWidth: 260 }}>{step.instruction ?? ""}</TableCell>
                      <TableCell>
                        {step.critical_action_type
                          ? copy(
                              pageContract,
                              step.critical_action_type === "lifecycle_exit"
                                ? "label.critical.exit"
                                : "label.critical.quarantine",
                            )
                          : ""}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </TableContainer>
        )}

        {(detail.history ?? []).length > 0 ? (
          <Box>
            <Typography variant="subtitle2" sx={{ mb: 0.5 }}>{copy(pageContract, "section.history.title")}</Typography>
            <Typography variant="body2" sx={{ color: "text.secondary", mb: 1 }}>
              {copy(pageContract, "section.history.note")}
            </Typography>
            <Box component="ul" sx={{ m: 0, pl: 2.25, typography: "body2", lineHeight: 1.8 }}>
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
            </Box>
          </Box>
        ) : null}
      </Stack>
    </Card>
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

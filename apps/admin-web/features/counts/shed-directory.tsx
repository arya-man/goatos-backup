import { redirect } from "next/navigation";

import { control, controlEnabled, copy, table, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getShedDirectory,
  listAnimalStages,
  type ShedDirectoryResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { one, type RouteSearchParams } from "@/lib/search-params";
import {
  paginationFromParams,
  VaccinationTablePager,
  type VaccinationPageSize,
} from "@/features/preventive-care-vaccination";

import { ShedDirectoryTable } from "./shed-directory-table";
import type { StageOption } from "./shed-stage-drawer";

type AnimalStageOptionItem = {
  stage_code: string;
  name?: string | null;
  age_band?: string | null;
  assignable_as_cohort?: boolean;
};

const PAGE_PATH = "/counts/sheds";
const DEFAULT_PAGE_SIZE = 25;

// Counts -> Sheds. The shed CONFIGURATION directory.
//
// It answers a different question from its two neighbours, and the note under the table says so
// out loud because the three are easy to confuse: Herd Register lists ANIMALS, Counts Breakdown
// counts animals BY LOCATION, and this lists the LOCATIONS THEMSELVES — what the farm has built,
// the cohort each shed is configured for, and the head count it is meant to hold. A shed holding
// zero animals still belongs here.
//
// GRAIN is the OPERATIONAL LOCATION — a pen where the shed has pens ("Godel 1 - Part 3"), the bare
// shed where it has none ("Q1") — the same grain the farm's own Sheds DB sheet records. Rows pair
// across parks by that label, because the farm runs the same names in both parks and reads them
// side by side. Pens come from the shed_partitions catalog, so an empty pen is still listed; legacy
// partition-alias location rows are excluded backend-side, so a pen appears once, not twice.
//
// CAPACITY is the pen's own, never the shed's total repeated across its pens. TAG has no pen-grain
// source in the database (shed_profiles is keyed by the shed), so a pen reports its shed's
// configured cohort — the note says so rather than leaving a reader to assume each pen was tagged
// separately. Neither column is sortable: they are per-park values on a row whose identity is the
// location, and ranking one park's column would imply a comparison the other does not share.
//
// No filters, and not park-scoped: showing the parks side by side is the entire point of the
// screen, so narrowing it to one park would defeat it. It DOES page — the catalog is ~120 pens and
// a single unbroken table ran past the viewport several times over — and `total_rows` under the
// pager stays the whole catalog rather than the page length.

export async function CountsShedsPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const pageSizeOptions = tablePageSizes(pageContract, "shed-directory");
  const requestedLimit = Number(one(sp, "sd_limit"));
  const pageSize: VaccinationPageSize = pageSizeOptions.includes(requestedLimit)
    ? requestedLimit
    : DEFAULT_PAGE_SIZE;
  const requestedPage = Math.max(1, Number(one(sp, "sd_page")) || 1);

  // One fan-out, not a serial await: the stage vocabulary does not depend on the directory, and
  // the inline tag editor needs it on first paint rather than on first click.
  const [directoryResult, stageResult] = await Promise.all([
    getShedDirectory({ limit: pageSize, offset: (requestedPage - 1) * pageSize }),
    listAnimalStages(),
  ]);

  const authError = firstAuthRequiredError(directoryResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const directory: ShedDirectoryResponse | null = directoryResult.ok ? directoryResult.data : null;
  const rows = directory?.items ?? [];

  // Column keys, labels and order all come from the compiled contract, including the per-park
  // pairs the backend built from the live park rows. This page declares no column list.
  const directoryTable = table(pageContract, "shed-directory");

  // The tenant's active stage vocabulary, business-managed in Postgres. `name` is the human label
  // and `stage_code` is what the write sends -- never a constant list here, so adding a cohort tag
  // does not need a frontend release.
  // Clinical tags (ICU, Quarantine) are dropped: they describe an animal's medical state, belong to
  // the clinical flows, and the retag write rejects them -- offering one and failing on apply is
  // worse than not offering it. The backend decides which those are, so the clinical set is not
  // copied into this file.
  const stageOptions: StageOption[] = (stageResult.ok ? stageResult.data.items : [])
    .filter((item: AnimalStageOptionItem) => item.assignable_as_cohort !== false)
    .map((item: AnimalStageOptionItem) => ({
      code: item.stage_code,
      // The CODE is the tag: it is what the table cell shows, what the farm's own sheet uses, and
      // what the write stores. The lookup's descriptive name rides along as context.
      label: item.stage_code,
      // Case-insensitive: "Non-Pregnant" and "Non-pregnant" are the same word, and repeating it
      // under the tag is noise rather than help.
      description:
        (item.name ?? "").toLowerCase() === item.stage_code.toLowerCase() ? "" : (item.name ?? ""),
      band: item.age_band ?? "",
      assignable: true,
    }));

  // Authority is the backend's answer, read off the compiled control -- the same control id the
  // Counts Breakdown drawer uses, because it is the same write behind the same permission.
  const retagEnabled = controlEnabled(pageContract, "change_shed_stage", false);
  const retagDisabledReason = control(pageContract, "change_shed_stage").disabled_reason ?? "";

  // The pager's total is the backend's whole-catalog figure, never rows.length — recomputing it
  // from the visible page would report a page subtotal as business truth.
  const pagination = paginationFromParams(sp, "sd", directory?.total_rows ?? 0, DEFAULT_PAGE_SIZE, pageSizeOptions);

  function hrefWithParam(key: string, value: string): string {
    const next = new URLSearchParams();
    for (const [paramKey, paramValue] of Object.entries(sp)) {
      if (paramKey === key) continue;
      if (Array.isArray(paramValue)) {
        for (const item of paramValue) if (item) next.append(paramKey, item);
      } else if (paramValue) {
        next.set(paramKey, paramValue);
      }
    }
    if (value) next.set(key, value);
    const qs = next.toString();
    return qs ? `${PAGE_PATH}?${qs}` : PAGE_PATH;
  }

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.sheds.title")}</b>
          </div>
          <h1>{pageContract.title}</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {/* An API failure surfaces as a visible error band. An empty table would read to an operator
          as "this farm has no sheds", which is never true and would send them looking in the wrong
          place. */}
      {!directoryResult.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{directoryResult.error.code ?? directoryResult.error.kind}</b>&nbsp;{directoryResult.error.message}
        </div>
      ) : null}

      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <h3>{copy(pageContract, "section.sheds.title")}</h3>
          <span className="small muted">{copy(pageContract, "section.sheds.caption")}</span>
        </div>

        <div
          className="bd"
          style={{ padding: 0, overflowX: "auto" }}
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.sheds.aria")}
        >
          <ShedDirectoryTable
            contract={directoryTable}
            pageContract={pageContract}
            rows={rows}
            stages={stageOptions}
            retagEnabled={retagEnabled}
            retagDisabledReason={retagDisabledReason}
            ariaLabel={copy(pageContract, "table.sheds.aria")}
            noTagLabel={copy(pageContract, "value.no_tag")}
            noCapacityLabel={copy(pageContract, "value.no_capacity")}
            notInParkLabel={copy(pageContract, "value.not_in_park")}
            empty={
              <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                {directoryResult.ok ? (
                  <>
                    <b>{copy(pageContract, "empty.title")}</b>
                    <br />
                    {copy(pageContract, "empty.body")}
                  </>
                ) : (
                  copy(pageContract, "empty.title")
                )}
              </div>
            }
          />
        </div>
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={pagination.page}
          pageSize={pagination.pageSize}
          total={pagination.total}
          start={pagination.start}
          end={pagination.end}
          noun={copy(pageContract, "table.sheds.noun")}
          hrefForPage={(nextPage) => hrefWithParam("sd_page", String(nextPage))}
          hrefForPageSize={(nextSize) => hrefWithParam("sd_limit", String(nextSize))}
        />
      </section>

      <div className="note">{copy(pageContract, "section.sheds.note")}</div>
    </div>
  );
}

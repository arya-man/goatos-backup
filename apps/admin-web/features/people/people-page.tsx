import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";
import Link from "@/components/no-prefetch-link";
import { PeopleBoard } from "./people-board";
import { VaccinationOperatorsScreen } from "./vaccination-operators-screen";
import { ClockScreen } from "./clock-screen";

/**
 * The /people shell: page header + the backend-owned `people_view_tabs` module
 * strip, hosting the ALL-PEOPLE directory (default) and the Vaccination
 * operators screen. Disabled tabs render inert with their backend-declared
 * reason — the strip's membership, labels, and availability all come from the
 * page contract, never from this file.
 */
export async function PeoplePage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  // The clock tab is capability-gated (clock.presence.read) through the
  // view_clock control, per role-scoped-UI rules: the strip's membership comes
  // from the option group, the per-principal gate from the compiled control.
  const clockAllowed = controlEnabled(pageContract, "view_clock", true);
  const tabs = optionGroup(pageContract, "people_view_tabs").map((tab) =>
    tab.key === "clock" && !clockAllowed
      ? { ...tab, enabled: false, disabled_reason: copy(pageContract, "clock.tab.locked") }
      : tab,
  );
  const requested = one(searchParams, "tab") ?? "all";
  const active = tabs.find((tab) => tab.key === requested && tab.enabled)?.key ?? "all";

  const park = one(searchParams, "park");
  const initialParkId = park && park !== "all" ? park : undefined;

  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="crumb">
            <b>{copy(pageContract, "crumb")}</b> · {pageContract.title}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
      </div>

      <nav className="subtabs" aria-label={pageContract.title}>
        {tabs.map((tab) =>
          tab.enabled ? (
            <Link
              key={tab.key}
              href={tab.key === "all" ? "/people" : `/people?tab=${tab.key}`}
              className={tab.key === active ? "on" : undefined}
              aria-current={tab.key === active ? "page" : undefined}
              scroll={false}
            >
              {tab.label}
            </Link>
          ) : (
            <button
              key={tab.key}
              type="button"
              className="disabled"
              disabled
              aria-disabled="true"
              title={tab.disabled_reason}
            >
              {tab.label}
            </button>
          ),
        )}
      </nav>

      {active === "vaccination" ? (
        <VaccinationOperatorsScreen initialParkId={initialParkId} pageContract={pageContract} />
      ) : active === "clock" ? (
        <ClockScreen searchParams={searchParams} pageContract={pageContract} />
      ) : (
        <PeopleBoard searchParams={searchParams} pageContract={pageContract} />
      )}
    </div>
  );
}

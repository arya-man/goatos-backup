import type { ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { PeopleBoard } from "./people-board";
import { ClockScreen } from "./clock-screen";
import { NotificationsScreen } from "./notifications-screen";
import { getAdminWebBootstrap } from "@/lib/api/server";

/**
 * The HRMS pages (maintainer request 2026-09-30). People / HRMS used to be ONE page whose views
 * sat behind a tab strip; each view is now its own page and sidebar leaf in the HRMS group, with
 * its own page contract. The four People pages share the People copy map, so they render the
 * words they always did. Every visible word is backend copy.
 */
export function HrmsPageFrame({ pageContract, children }: { pageContract: AdminUiPageContract; children: ReactNode }) {
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
      {children}
    </div>
  );
}

/** /people: the ALL-PEOPLE directory and the Add Person drawer. */
export function PeoplePage({ searchParams, pageContract }: { searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  return (
    <HrmsPageFrame pageContract={pageContract}>
      <PeopleBoard searchParams={searchParams} pageContract={pageContract} />
    </HrmsPageFrame>
  );
}

/** /people/clock: who clocked in and out on the selected day. */
export function PeopleClockPage({ searchParams, pageContract }: { searchParams: RouteSearchParams; pageContract: AdminUiPageContract }) {
  return (
    <HrmsPageFrame pageContract={pageContract}>
      <ClockScreen searchParams={searchParams} pageContract={pageContract} />
    </HrmsPageFrame>
  );
}

/** /people/notifications: which job titles hear each alert. */
export function PeopleNotificationsPage({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <HrmsPageFrame pageContract={pageContract}>
      <NotificationsScreen pageContract={pageContract} />
    </HrmsPageFrame>
  );
}

/** /people/vaccination: the vaccination operators per park. */
export async function PeopleVaccinationPage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  // The screen names the park it is scoped to. The names come from the same backend-compiled park
  // list the top bar uses (Configuration > Items & settings > Parks), never a literal in the page.
  const [mod, bootstrap] = await Promise.all([import("./vaccination-operators-screen"), getAdminWebBootstrap()]);
  const parks = bootstrap.ok
    ? bootstrap.data.top_bar.park_selector.options.map((option) => ({
        parkId: option.key,
        code: option.label,
        name: option.title || option.label,
      }))
    : [];
  const park = one(searchParams, "park");
  const initialParkId = park && park !== "all" ? park : undefined;
  // The operators screen carries its own page header (crumb, title, park scope line), so it is
  // not wrapped in the HRMS frame -- that stacked two "Vaccination operators" headings.
  return <mod.VaccinationOperatorsScreen initialParkId={initialParkId} pageContract={pageContract} parks={parks} />;
}

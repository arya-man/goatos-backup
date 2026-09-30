import type { ReactNode } from "react";
import { PageRoot } from "@/components/app/page-root";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { FilterCardSkeleton, TableSkeleton, TabsSkeleton } from "@/components/app/skeletons";
import { controlEnabled, copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { PageHeader } from "@/components/app/page-header";
import { TemplateTabs, TabPanel } from "@/components/app/template-tabs";
import { PeopleBoard } from "./people-board";
import { PeopleAddButton } from "./people-add-button";
import { ClockScreen } from "./clock-screen";
import { NotificationsScreen } from "./notifications-screen";
import { VaccinationDeskSkeleton, vaccinationDeskOpensOnChooser } from "./people-skeletons";
import { getAdminWebBootstrap } from "@/lib/api/server";

const VaccinationOperatorsScreen = async ({
  initialParkId,
  pageContract,
}: {
  initialParkId?: string;
  pageContract: AdminUiPageContract;
}) => {
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
  return <mod.VaccinationOperatorsScreen initialParkId={initialParkId} pageContract={pageContract} parks={parks} />;
};

/**
 * The /people shell: page header + the backend-owned `people_view_tabs` module
 * strip, hosting the ALL-PEOPLE directory (default) and the Vaccination
 * operators screen. The strip's membership, labels, and availability all come
 * from the page contract, never from this file. A tab the contract (or the
 * view_clock gate) disables is NOT rendered: an inert "soon" tab is a dead
 * control (TR-2 P1-4, guard: no-disabled-contract-tabs).
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
  const tabs = optionGroup(pageContract, "people_view_tabs").filter((tab) => tab.enabled && (tab.key !== "clock" || clockAllowed));
  const requested = one(searchParams, "tab") ?? "all";
  const active = tabs.find((tab) => tab.key === requested)?.key ?? "all";

  const park = one(searchParams, "park");
  const initialParkId = park && park !== "all" ? park : undefined;
  // The Vaccination desk's skeleton follows what it will land on (chooser card or roster).
  const bootstrap = await getAdminWebBootstrap();
  const parkCount = bootstrap.ok ? bootstrap.data.top_bar.park_selector.options.length : 0;
  const deskSkeleton: Record<string, ReactNode> = {
    ...DESK_SKELETON,
    vaccination: <VaccinationDeskSkeleton chooser={vaccinationDeskOpensOnChooser(initialParkId, parkCount)} />,
  };

  return (
    <PageRoot>
      <div>
        {/* Template user list: the primary "Add" action sits on the breadcrumbs row. */}
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
          actions={<PeopleAddButton href={addPersonHref(searchParams, active)} label={copy(pageContract, "action.add_person")} overlay={active === "all"} />}
        />
      </div>

      {tabs.length > 1 ? (
        <TemplateTabs
          ariaLabel={pageContract.title}
          value={active}
          items={tabs.map((tab) => ({
            value: tab.key,
            label: tab.label,
            href: tab.key === "all" ? "/people" : `/people?tab=${tab.key}`,
          }))}
        />
      ) : null}

      {/* The four People desks are whole screens, and switching used to swap them instantly with a
          height jump. TabPanel cross-fades the old screen out and rises the new one in against a
          measured height, so the tab strip above it never moves under the pointer. */}
      {/* Each desk streams (guard: url-keyed-panel): a tab click shows the clicked desk's skeleton in
          the same frame; header and strip stay on screen. */}
      <UrlSuspense searchParams={searchParams} watch={["tab", "park"]} fallback={deskSkeleton[active] ?? deskSkeleton.all} fallbackBy={{ param: "tab", shapes: { ...deskSkeleton, "": deskSkeleton.all } }}>
      <TabPanel tabKey={active}>
        {active === "vaccination" ? (
          <VaccinationOperatorsScreen initialParkId={initialParkId} pageContract={pageContract} />
        ) : active === "clock" ? (
          <ClockScreen searchParams={searchParams} pageContract={pageContract} />
        ) : active === "notifications" ? (
          <NotificationsScreen pageContract={pageContract} />
        ) : (
          <PeopleBoard searchParams={searchParams} pageContract={pageContract} />
        )}
      </TabPanel>
      </UrlSuspense>
    </PageRoot>
  );
}

/** Each desk's skeleton, from the shared blocks. */
const DESK_SKELETON: Record<string, ReactNode> = {
  all: <TableSkeleton columns={6} rows={10} header={false} tabs={<TabsSkeleton count={4} counts />} toolbar={<FilterCardSkeleton inCard fields={[200, 200, "search"]} />} />,
  clock: <PanelSkeleton kpis={3} table={10} />,
  notifications: <PanelSkeleton table={8} />,
};

/**
 * "Add person" stays on EVERY desk (J3 P1-1: rendering it only on All People collapsed the header row
 * and moved the tab strip ~60px under the thumb). On All People it opens the drawer in place; on the
 * other desks it navigates to All People with the drawer open (the drawer lives on the directory).
 * guard: people-header-action-every-desk (features/people/people-page-header.test.mjs)
 */
function addPersonHref(sp: RouteSearchParams, active: string): string {
  const query = new URLSearchParams();
  if (active !== "all") {
    const park = one(sp, "park");
    if (park) query.set("park", park);
    query.set("person", "new");
    return `/people?${query.toString()}`;
  }
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  query.set("person", "new");
  return `/people?${query.toString()}`;
}

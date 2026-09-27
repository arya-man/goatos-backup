import type { ReactNode } from "react";
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
    <div className="kit-enter screen on">
      <div>
        {/* Template user list: the primary "Add" action sits on the breadcrumbs row. */}
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]}
          actions={active === "all" ? <PeopleAddButton href={addPersonHref(searchParams)} label={copy(pageContract, "action.add_person")} /> : undefined}
        />
      </div>

      <TemplateTabs
        ariaLabel={pageContract.title}
        value={active}
        items={tabs.map((tab) => ({
          value: tab.key,
          label: tab.label,
          disabled: !tab.enabled,
          href: tab.enabled ? (tab.key === "all" ? "/people" : `/people?tab=${tab.key}`) : undefined,
        }))}
      />

      {/* The four People desks are whole screens, and switching used to swap them instantly with a
          height jump. TabPanel cross-fades the old screen out and rises the new one in against a
          measured height, so the tab strip above it never moves under the pointer. */}
      {/* Each desk streams (guard: url-keyed-panel): a tab click shows the clicked desk's skeleton in
          the same frame; header and strip stay on screen. */}
      <UrlSuspense searchParams={searchParams} watch={["tab", "park"]} fallback={DESK_SKELETON[active] ?? DESK_SKELETON.all} fallbackBy={{ param: "tab", shapes: { ...DESK_SKELETON, "": DESK_SKELETON.all } }}>
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
    </div>
  );
}

/** Each desk's skeleton, from the shared blocks. */
const DESK_SKELETON: Record<string, ReactNode> = {
  all: <TableSkeleton columns={6} rows={10} header={false} tabs={<TabsSkeleton count={4} counts />} toolbar={<FilterCardSkeleton inCard fields={[200, 200, "search"]} />} />,
  vaccination: <PanelSkeleton kpis={4} table={10} />,
  clock: <PanelSkeleton kpis={3} table={10} />,
  notifications: <PanelSkeleton table={8} />,
};

function addPersonHref(sp: RouteSearchParams): string {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(sp)) {
    const single = Array.isArray(value) ? value[0] : value;
    if (single) query.set(key, single);
  }
  query.set("person", "new");
  return `/people?${query.toString()}`;
}

import Link from "next/link";
import { randomUUID } from "node:crypto";
import { redirect } from "next/navigation";
import { AlertTriangle, Building2, CheckCircle2, MapPinned, PlusCircle, Tags } from "lucide-react";
import {
  ActionNotice,
  EmptyPanel,
  ErrorPanel,
  FormField,
  FormSelect,
  FormTextArea,
  NextPageLink,
  PageHeader,
  Panel,
  RowsPerPageSelect,
  ValueList,
} from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { DialogModal } from "@/components/dialog-modal";
import { formatLabel } from "@/lib/display-utils";
import { dash, dateTime, joinParts, shortId } from "@/lib/format";
import { formAction } from "@/lib/routes";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import {
  firstAuthRequiredError,
  getLocation,
  getLocationUsage,
  listLocationAliases,
  listLocationCapacity,
  listLocationChildren,
  listLocationReviewItems,
  listLocations,
  type ApiUiError,
  type LocationAliasListResponse,
  type LocationCapacityListResponse,
  type LocationListResponse,
  type LocationResponse,
  type LocationReviewListResponse,
  type LocationUsageResponse,
} from "@/lib/api/server";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import {
  createLocationAction,
  createLocationAliasAction,
  createLocationCapacityAction,
  deleteLocationAction,
  deleteLocationAliasAction,
  deleteLocationCapacityAction,
  resolveLocationReviewItemAction,
  retireLocationAction,
  retireLocationAliasAction,
  updateLocationAliasAction,
  updateLocationCapacityAction,
  updateLocationAction,
} from "./actions";

type LocationRow = LocationListResponse["items"][number];
type ReviewRow = LocationReviewListResponse["items"][number];
type AliasRow = LocationAliasListResponse["items"][number];
type CapacityRow = LocationCapacityListResponse["items"][number];

type SelectedLocationData = {
  location: LocationResponse["location"] | null;
  aliases: AliasRow[];
  capacity: CapacityRow[];
  usage: LocationUsageResponse | null;
  children: LocationRow[];
  errors: ApiUiError[];
};

const locationTypes = ["farm", "park", "shed", "cohort", "pen", "unknown"];
const locationStatuses = ["active", "inactive", "staging", "review"];
const aliasContexts = ["legacy_location_code", "legacy_bq_dashboard_shed", "legacy_bq_counts", "legacy_bq_mortality", "counts_source", "mortality_source", "manual", "import"];
const capacityKinds = ["goat_occupancy", "quarantine", "feed_trial", "other"];
const capacitySources = ["manual", "legacy_bq", "android_sop", "import"];

export async function LocationsPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const selectedLocationID = one(searchParams, "location_id");
  const selectedReviewID = one(searchParams, "review_id");
  const createModalOpen = one(searchParams, "create") === "location";
  const limit = boundedInt(one(searchParams, "limit"), 25, 10, 100);
  const page = boundedInt(one(searchParams, "page"), 1, 1, 1000000);
  const offset = (page - 1) * limit;
  const params = {
    limit: limit + 1,
    offset,
    search: one(searchParams, "search"),
    status: one(searchParams, "status") ?? "active",
    type: one(searchParams, "type"),
  };
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
  const [locations, reviewItems, detail, aliases, capacity, usage, children] = await Promise.all([
    listLocations(params),
    listLocationReviewItems({ limit: 25, status: "open" }),
    selectedLocationID ? getLocation(selectedLocationID) : Promise.resolve(null),
    selectedLocationID ? listLocationAliases(selectedLocationID, 100) : Promise.resolve(null),
    selectedLocationID ? listLocationCapacity(selectedLocationID, 100) : Promise.resolve(null),
    selectedLocationID ? getLocationUsage(selectedLocationID) : Promise.resolve(null),
    selectedLocationID ? listLocationChildren(selectedLocationID, 100) : Promise.resolve(null),
  ]);
  const authError = firstAuthRequiredError(locations, reviewItems, detail, aliases, capacity, usage, children);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  const selected: SelectedLocationData = {
    location: detail?.ok ? detail.data.location : null,
    aliases: aliases?.ok ? aliases.data.items : [],
    capacity: capacity?.ok ? capacity.data.items : [],
    usage: usage?.ok ? usage.data : null,
    children: children?.ok ? children.data.items : [],
    errors: [detail, aliases, capacity, usage, children].flatMap((result) => (result && !result.ok ? [result.error] : [])),
  };

  return (
    <>
      <PageHeader
        eyebrow="Locations"
        title="Location Master"
        description="Canonical farms, parks, sheds, aliases, capacity, and unresolved location review items."
        actions={
          <Link
            href={createLocationHref(searchParams)}
            scroll={false}
            className="inline-flex h-10 items-center gap-2 rounded-lg bg-[#14f1d9] px-3 text-sm font-bold text-[#081015] hover:bg-[#5ff7e8]"
          >
            <PlusCircle className="h-4 w-4" aria-hidden="true" />
            Create location
          </Link>
        }
      />
      <ActionNotice status={actionStatus} message={actionMessage} />

      <form className="mb-5 grid grid-cols-1 gap-3 rounded-xl border border-[#334155] bg-[#1A1D24] p-4 md:grid-cols-[minmax(0,1fr)_160px_160px_120px_auto]" action={formAction("/locations")}>
        <FormField name="search" label="Search" defaultValue={params.search} placeholder="Name, code, alias" />
        <FormField name="type" label="Type" defaultValue={params.type} placeholder="farm, park, shed" />
        <FormField name="status" label="Status" defaultValue={params.status} placeholder="active" />
        <RowsPerPageSelect defaultValue={String(limit)} options={[10, 25, 50, 100]} />
        <button
          type="submit"
          className="mt-5 inline-flex h-10 items-center justify-center rounded-lg border border-[#14F1D9]/50 px-4 text-sm font-bold text-[#14F1D9] hover:border-[#14F1D9] md:mt-6"
        >
          Apply
        </button>
      </form>

      {!locations.ok ? (
        <ErrorPanel error={locations.error} />
      ) : (
        <LocationsBody
          locations={locations.data.items.slice(0, limit)}
          hasNextPage={locations.data.items.length > limit}
          page={page}
          limit={limit}
          selected={selected}
          selectedLocationID={selectedLocationID}
          selectedReviewID={selectedReviewID}
          createModalOpen={createModalOpen}
          searchParams={searchParams}
          reviewItems={reviewItems.ok ? reviewItems.data.items : []}
          reviewError={!reviewItems.ok ? reviewItems.error : null}
        />
      )}
    </>
  );
}

function LocationsBody({
  locations,
  hasNextPage,
  page,
  limit,
  selected,
  selectedLocationID,
  selectedReviewID,
  createModalOpen,
  searchParams,
  reviewItems,
  reviewError,
}: {
  locations: LocationRow[];
  hasNextPage: boolean;
  page: number;
  limit: number;
  selected: SelectedLocationData;
  selectedLocationID?: string;
  selectedReviewID?: string;
  createModalOpen: boolean;
  searchParams: RouteSearchParams;
  reviewItems: ReviewRow[];
  reviewError: ApiUiError | null;
}) {
  const active = locations.filter((item) => item.status === "active").length;
  const aliases = locations.reduce((sum, item) => sum + item.alias_count, 0);
  const withCapacity = locations.filter((item) => item.current_capacity !== null).length;
  const unusable = locations.filter((item) => !item.operational.usable_for_counts).length;
  const selectedReview = selectedReviewID ? reviewItems.find((item) => item.review_id === selectedReviewID) ?? null : null;
  const closeReviewHref = locationHref(searchParams, selectedLocationID);
  const closeCreateHref = locationHref(searchParams, selectedLocationID);

  return (
    <>
      <DialogModal open={createModalOpen} closeHref={closeCreateHref} label="Create location">
        <div className="p-5">
          <CreateLocationPanel locations={locations} closeHref={closeCreateHref} />
        </div>
      </DialogModal>

      <DialogModal open={Boolean(selectedReviewID)} closeHref={closeReviewHref} label="Location review queue">
        <div className="p-5">
          <ReviewQueueModal
            reviewItems={reviewItems}
            selectedReview={selectedReview}
            reviewError={reviewError}
            locations={locations}
            searchParams={searchParams}
            closeHref={closeReviewHref}
          />
        </div>
      </DialogModal>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Page rows" value={locations.length} detail={`${active.toLocaleString("en-IN")} active on this page`} icon={<MapPinned size={18} />} tone={locations.length === 0 ? "warn" : "good"} />
        <StatCard label="Page aliases" value={aliases} detail="active source labels" icon={<Tags size={18} />} />
        <StatCard label="Page capacity" value={withCapacity} detail="with current records" icon={<Building2 size={18} />} />
        <ReviewStatCard reviewItems={reviewItems} unusable={unusable} searchParams={searchParams} />
      </div>

      <div className="mt-6 grid grid-cols-1 gap-5 xl:grid-cols-[minmax(0,1.4fr)_minmax(440px,1fr)]">
        <div className="min-w-0 space-y-5">
          <Panel title="Locations" description={`Page ${page.toLocaleString("en-IN")} · ${limit.toLocaleString("en-IN")} rows per page`}>
            {locations.length === 0 ? (
              <EmptyPanel message="No locations returned." />
            ) : (
              <div className="space-y-3">
                <LocationRows locations={locations} selectedLocationID={selectedLocationID} searchParams={searchParams} />
                <NextPageLink
                  href={hasNextPage ? locationPageHref(searchParams, page + 1, limit) : null}
                  previousHref={page > 1 ? locationPageHref(searchParams, page - 1, limit) : null}
                  currentPage={page}
                  pageSize={limit}
                  itemCount={locations.length}
                />
              </div>
            )}
          </Panel>
        </div>

        <div className="min-w-0 space-y-5">
          <LocationWorkbench selected={selected} locations={locations} />
          <ReviewQueueLauncher reviewItems={reviewItems} reviewError={reviewError} searchParams={searchParams} />
        </div>
      </div>
    </>
  );
}

function LocationRows({
  locations,
  selectedLocationID,
  searchParams,
}: {
  locations: LocationRow[];
  selectedLocationID?: string;
  searchParams: RouteSearchParams;
}) {
  return (
    <div className="overflow-x-auto">
      <div className="min-w-[680px] overflow-hidden rounded-lg border border-[#334155] text-sm">
        <div className="grid grid-cols-[minmax(220px,1.4fr)_90px_130px_minmax(210px,1fr)] border-b border-[#334155] bg-[#10141b] px-3 py-2 text-xs font-semibold uppercase text-[#8899AA]">
          <span>Name</span>
          <span>Type</span>
          <span>Parent</span>
          <span>Capacity / use</span>
        </div>
        <div className="divide-y divide-[#334155]">
          {locations.map((location) => {
            const selected = location.location_id === selectedLocationID;
            return (
              <Link
                key={location.location_id}
                href={locationHref(searchParams, location.location_id)}
                scroll={false}
                aria-current={selected ? "page" : undefined}
                className={
                  selected
                    ? "grid grid-cols-[minmax(220px,1.4fr)_90px_130px_minmax(210px,1fr)] items-center gap-0 bg-[#10141b] px-3 py-3 text-[#E0E8F0] outline-none ring-1 ring-[#14f1d9]/40 hover:bg-[#121923] focus-visible:ring-2 focus-visible:ring-[#14f1d9]"
                    : "grid grid-cols-[minmax(220px,1.4fr)_90px_130px_minmax(210px,1fr)] items-center gap-0 px-3 py-3 text-[#E0E8F0] outline-none hover:bg-[#121923] focus-visible:ring-2 focus-visible:ring-[#14f1d9]"
                }
              >
                <span className="min-w-0 pr-3">
                  <span className="block truncate font-semibold text-white">{location.name}</span>
                  <span className="mt-1 block truncate text-xs text-[#8899AA]">{joinParts([location.location_code, shortId(location.location_id), location.status])}</span>
                </span>
                <span>{formatLabel(location.location_type)}</span>
                <span className="truncate text-[#B0BEC5]">{dash(location.parent_name)}</span>
                <span className="flex min-w-0 items-center justify-between gap-3">
                  <span className="shrink-0 text-right tabular-nums">{location.current_capacity === null ? "—" : location.current_capacity.toLocaleString("en-IN")}</span>
                  <OperationalFlags location={location} />
                </span>
              </Link>
            );
          })}
        </div>
      </div>
    </div>
  );
}

function CreateLocationPanel({ locations, closeHref }: { locations: LocationRow[]; closeHref: string }) {
  return (
    <Panel
      title="Create Location"
      description="Creates the core location and operational flags together. Initial alias and capacity are attached as child records after the location exists."
      action={
        <Link href={closeHref} scroll={false} className="text-sm font-semibold text-[#14f1d9] hover:text-white">
          Close
        </Link>
      }
    >
      <form action={createLocationAction} className="space-y-3">
        <input type="hidden" name="idempotency_key" value={randomUUID()} />
        <input type="hidden" name="return_to" value="/locations" />
        <div className="rounded-lg border border-[#334155] bg-[#10141b] p-3">
          <div className="text-sm font-semibold text-white">Core Details</div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <FormField name="name" label="Name" required />
            <FormField name="location_code" label="Code" />
            <FormSelect name="location_type" label="Type" options={locationTypes} required emptyLabel="Select" defaultValue="shed" />
            <FormSelect name="status" label="Status" options={locationStatuses} required emptyLabel="Select" defaultValue="staging" />
            <LocationSelect name="parent_location_id" label="Parent" locations={locations} />
            <FormField name="district" label="District" />
            <FormField name="country" label="Country" defaultValue="IN" />
            <FormField name="timezone" label="Timezone" defaultValue="Asia/Kolkata" />
          </div>
        </div>
        <div className="rounded-lg border border-[#334155] bg-[#10141b] p-3">
          <div className="text-sm font-semibold text-white">Operational Flags</div>
          <div className="mt-3">
            <OperationalFormFields />
          </div>
        </div>
        <div className="rounded-lg border border-[#334155] bg-[#10141b] p-3">
          <div className="text-sm font-semibold text-white">Initial Alias</div>
          <div className="mt-1 text-xs text-[#93a4b8]">Optional child record. Leave blank if the alias should be added later.</div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_190px]">
            <FormField name="initial_alias_code" label="Alias" placeholder="legacy shed/code label" />
            <FormSelect name="initial_alias_context" label="Context" options={aliasContexts} required emptyLabel="Select" defaultValue="manual" />
            <div className="sm:col-span-2">
              <FormTextArea name="initial_alias_notes" label="Alias notes" rows={2} />
            </div>
          </div>
        </div>
        <div className="rounded-lg border border-[#334155] bg-[#10141b] p-3">
          <div className="text-sm font-semibold text-white">Initial Capacity</div>
          <div className="mt-1 text-xs text-[#93a4b8]">Optional effective-dated child record. Fill value and from date to create it.</div>
          <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
            <FormSelect name="initial_capacity_kind" label="Kind" options={capacityKinds} required emptyLabel="Select" defaultValue="goat_occupancy" />
            <FormField name="initial_capacity_value" label="Value" type="number" min="1" />
            <FormField name="initial_capacity_from" label="From" type="date" />
            <FormField name="initial_capacity_to" label="To" type="date" />
            <FormSelect name="initial_capacity_source" label="Source" options={capacitySources} required emptyLabel="Select" defaultValue="manual" />
            <FormField name="initial_capacity_source_ref" label="Source ref" />
            <div className="sm:col-span-2">
              <FormTextArea name="initial_capacity_notes" label="Capacity notes" rows={2} />
            </div>
          </div>
        </div>
        <div className="flex justify-end">
          <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Create location</button>
        </div>
      </form>
    </Panel>
  );
}

function LocationWorkbench({ selected, locations }: { selected: SelectedLocationData; locations: LocationRow[] }) {
  if (!selected.location && selected.errors.length === 0) {
    return <EmptyPanel message="Select a location to inspect detail, aliases, capacity, usage, and children." />;
  }
  if (selected.errors.length > 0) {
    return <ErrorPanel error={selected.errors[0]} />;
  }
  if (!selected.location) {
    return <EmptyPanel message="Location not found." />;
  }

  const location = selected.location;
  const canAttemptHardDelete = location.status === "staging" || location.status === "review";
  return (
    <div className="space-y-5">
      <Panel title="Read / Update Details" description={joinParts([location.location_code, shortId(location.location_id), `row v${location.row_version}`])}>
        <form action={updateLocationAction} className="space-y-3">
          <input type="hidden" name="location_id" value={location.location_id} />
          <input type="hidden" name="row_version" value={location.row_version} />
          <input type="hidden" name="idempotency_key" value={randomUUID()} />
          <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <FormField name="name" label="Name" defaultValue={location.name} required />
            <FormField name="location_code" label="Code" defaultValue={location.location_code ?? ""} />
            <FormSelect name="location_type" label="Type" options={locationTypes} required emptyLabel="Select" defaultValue={location.location_type} />
            <FormSelect name="status" label="Status" options={locationStatuses} required emptyLabel="Select" defaultValue={location.status} />
            <LocationSelect name="parent_location_id" label="Parent" locations={locations.filter((item) => item.location_id !== location.location_id)} defaultValue={location.parent_location_id ?? ""} />
            <FormField name="district" label="District" defaultValue={location.district ?? ""} />
            <FormField name="country" label="Country" defaultValue={location.country} />
            <FormField name="timezone" label="Timezone" defaultValue={location.timezone} />
          </div>
          <OperationalFormFields location={location} />
          <div className="flex justify-end">
            <button className="h-10 rounded-md border border-[#14f1d9] px-3 text-sm font-semibold text-[#14f1d9] hover:bg-[#102018]">Update detail</button>
          </div>
        </form>
      </Panel>

      <Panel title="Delete / Retire" description={selected.usage?.has_blocking_usage ? "Blocking references exist; destructive changes are blocked." : "Retire keeps history. Hard delete is only for unreferenced staging/review rows."}>
        {selected.usage ? <UsageSummary usage={selected.usage} /> : <EmptyPanel message="Usage summary unavailable." />}
        <form action={retireLocationAction} className="mt-4 rounded-md border border-[#7f1d1d] bg-[#1d1214] p-3">
          <input type="hidden" name="location_id" value={location.location_id} />
          <input type="hidden" name="row_version" value={location.row_version} />
          <input type="hidden" name="idempotency_key" value={randomUUID()} />
          <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
          <FormTextArea name="reason" label="Retire reason" required rows={2} />
          <div className="mt-3 flex justify-end">
            <ConfirmSubmitButton
              message="Delete/retire this location? The backend blocks active references and keeps historical audit data."
              className="h-10 rounded-md border border-[#f87171] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#2a1518]"
            >
              Delete / retire location
            </ConfirmSubmitButton>
          </div>
        </form>
        <form action={deleteLocationAction} className="mt-3 rounded-md border border-[#7f1d1d] bg-[#160f11] p-3">
          <input type="hidden" name="location_id" value={location.location_id} />
          <input type="hidden" name="row_version" value={location.row_version} />
          <input type="hidden" name="idempotency_key" value={randomUUID()} />
          <input type="hidden" name="return_to" value="/locations" />
          <div className="text-sm font-semibold text-white">Safe Hard Delete</div>
          <div className="mt-1 text-xs text-[#fca5a5]">
            Only staging/review locations with no aliases, capacity, children, goats, review rows, grants, source rows, or projections can be physically deleted.
          </div>
          <div className="mt-3">
            <FormTextArea name="reason" label="Delete reason" required rows={2} />
          </div>
          <div className="mt-3 flex justify-end">
            <ConfirmSubmitButton
              message="Hard-delete this staging/review location? The backend blocks any reference and records audit evidence."
              className={
                canAttemptHardDelete
                  ? "h-10 rounded-md border border-[#f87171] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#2a1518]"
                  : "h-10 cursor-not-allowed rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#64748b]"
              }
              disabled={!canAttemptHardDelete}
            >
              Hard delete location
            </ConfirmSubmitButton>
          </div>
        </form>
      </Panel>

      <AliasPanel location={location} aliases={selected.aliases} />
      <CapacityPanel location={location} capacity={selected.capacity} />
      <ChildrenPanel childLocations={selected.children} />
    </div>
  );
}

function AliasPanel({ location, aliases }: { location: LocationRow; aliases: AliasRow[] }) {
  return (
    <Panel title="Associated Data - Aliases" description={`${aliases.length.toLocaleString("en-IN")} decoupled source-label records`}>
      <form action={createLocationAliasAction} className="grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_180px_auto]">
        <input type="hidden" name="location_id" value={location.location_id} />
        <input type="hidden" name="idempotency_key" value={randomUUID()} />
        <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
        <FormField name="alias_code" label="Alias" required />
        <FormSelect name="source_context" label="Context" options={aliasContexts} required emptyLabel="Select" defaultValue="manual" />
        <div className="flex items-end">
          <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Add</button>
        </div>
      </form>
      <div className="mt-3 space-y-2">
        {aliases.length === 0 ? (
          <EmptyPanel message="No aliases returned." />
        ) : (
          aliases.map((alias) => (
            <div key={alias.alias_id} className="rounded-md border border-[#334155] bg-[#10141b] p-3">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="font-semibold text-white">{alias.alias_code}</div>
                  <div className="mt-1 text-xs text-[#93a4b8]">{joinParts([formatLabel(alias.source_context), alias.status, `row v${alias.row_version}`])}</div>
                </div>
                <span className="rounded border border-[#334155] px-2 py-1 text-[10px] uppercase text-[#93a4b8]">{alias.status}</span>
              </div>
              <form action={updateLocationAliasAction} className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-[minmax(0,1fr)_190px_auto]">
                <input type="hidden" name="location_id" value={location.location_id} />
                <input type="hidden" name="alias_id" value={alias.alias_id} />
                <input type="hidden" name="row_version" value={alias.row_version} />
                <input type="hidden" name="idempotency_key" value={randomUUID()} />
                <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
                <FormField name="alias_code" label="Alias" defaultValue={alias.alias_code} required />
                <FormSelect name="source_context" label="Context" options={aliasContexts} required emptyLabel="Select" defaultValue={alias.source_context} />
                <div className="flex items-end">
                  <button className="h-10 rounded-md border border-[#14f1d9] px-3 text-sm font-semibold text-[#14f1d9] hover:bg-[#102018]">Update</button>
                </div>
                <div className="sm:col-span-3">
                  <FormTextArea name="notes" label="Notes" defaultValue={alias.notes ?? ""} rows={2} />
                </div>
              </form>
              <div className="mt-3 flex flex-wrap justify-end gap-2">
                {alias.status === "active" ? (
                  <form action={retireLocationAliasAction} className="flex flex-wrap items-end gap-2">
                    <input type="hidden" name="location_id" value={location.location_id} />
                    <input type="hidden" name="alias_id" value={alias.alias_id} />
                    <input type="hidden" name="row_version" value={alias.row_version} />
                    <input type="hidden" name="idempotency_key" value={randomUUID()} />
                    <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
                    <input name="reason" required placeholder="reason" className="h-9 w-40 rounded-md border border-[#334155] bg-[#0f1115] px-2 text-xs text-white outline-none focus:border-[#14f1d9]" />
                    <ConfirmSubmitButton message="Retire this alias?" className="h-9 rounded-md border border-[#7f1d1d] px-2 text-xs font-semibold text-[#fecaca]">
                      Retire
                    </ConfirmSubmitButton>
                  </form>
                ) : null}
                <form action={deleteLocationAliasAction} className="flex flex-wrap items-end gap-2">
                  <input type="hidden" name="location_id" value={location.location_id} />
                  <input type="hidden" name="alias_id" value={alias.alias_id} />
                  <input type="hidden" name="row_version" value={alias.row_version} />
                  <input type="hidden" name="idempotency_key" value={randomUUID()} />
                  <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
                  <input name="reason" required placeholder="delete reason" className="h-9 w-40 rounded-md border border-[#334155] bg-[#0f1115] px-2 text-xs text-white outline-none focus:border-[#14f1d9]" />
                  <ConfirmSubmitButton
                    message="Hard-delete this alias? Active aliases must be retired first."
                    className={
                      alias.status === "active"
                        ? "h-9 cursor-not-allowed rounded-md border border-[#334155] px-2 text-xs font-semibold text-[#64748b]"
                        : "h-9 rounded-md border border-[#7f1d1d] px-2 text-xs font-semibold text-[#fecaca]"
                    }
                    disabled={alias.status === "active"}
                  >
                    Delete
                  </ConfirmSubmitButton>
                </form>
              </div>
            </div>
          ))
        )}
      </div>
    </Panel>
  );
}

function CapacityPanel({ location, capacity }: { location: LocationRow; capacity: CapacityRow[] }) {
  return (
    <Panel title="Associated Data - Capacity" description={`${capacity.length.toLocaleString("en-IN")} decoupled effective-dated records`}>
      <form action={createLocationCapacityAction} className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <input type="hidden" name="location_id" value={location.location_id} />
        <input type="hidden" name="idempotency_key" value={randomUUID()} />
        <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
        <FormSelect name="capacity_kind" label="Kind" options={capacityKinds} required emptyLabel="Select" defaultValue="goat_occupancy" />
        <FormField name="capacity_value" label="Value" type="number" min="1" required />
        <FormField name="effective_from" label="From" type="date" required />
        <FormField name="effective_to" label="To" type="date" />
        <FormSelect name="source" label="Source" options={capacitySources} required emptyLabel="Select" defaultValue="manual" />
        <FormField name="source_ref" label="Source ref" />
        <div className="sm:col-span-2">
          <FormTextArea name="notes" label="Notes" rows={2} />
        </div>
        <div className="sm:col-span-2 flex justify-end">
          <button className="h-10 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015]">Save capacity</button>
        </div>
      </form>
      <div className="mt-3 space-y-2">
        {capacity.length === 0 ? (
          <EmptyPanel message="No capacity records returned." />
        ) : (
          capacity.map((record) => (
            <div key={record.capacity_record_id} className="rounded-md border border-[#334155] bg-[#10141b] p-3 text-sm">
              <div className="font-semibold text-white">{record.capacity_value.toLocaleString("en-IN")} {formatLabel(record.capacity_kind)}</div>
              <div className="mt-1 text-xs text-[#93a4b8]">
                {joinParts([record.effective_from, record.effective_to ? `to ${record.effective_to}` : "open", formatLabel(record.source), `row v${record.row_version}`])}
              </div>
              <form action={updateLocationCapacityAction} className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
                <input type="hidden" name="location_id" value={location.location_id} />
                <input type="hidden" name="capacity_record_id" value={record.capacity_record_id} />
                <input type="hidden" name="row_version" value={record.row_version} />
                <input type="hidden" name="idempotency_key" value={randomUUID()} />
                <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
                <FormSelect name="capacity_kind" label="Kind" options={capacityKinds} required emptyLabel="Select" defaultValue={record.capacity_kind} />
                <FormField name="capacity_value" label="Value" type="number" min="1" defaultValue={String(record.capacity_value)} required />
                <FormField name="effective_from" label="From" type="date" defaultValue={record.effective_from} required />
                <FormField name="effective_to" label="To" type="date" defaultValue={record.effective_to ?? ""} />
                <FormSelect name="source" label="Source" options={capacitySources} required emptyLabel="Select" defaultValue={record.source} />
                <FormField name="source_ref" label="Source ref" defaultValue={record.source_ref ?? ""} />
                <div className="sm:col-span-2">
                  <FormTextArea name="notes" label="Notes" defaultValue={record.notes ?? ""} rows={2} />
                </div>
                <div className="sm:col-span-2 flex flex-wrap justify-end gap-2">
                  <button className="h-10 rounded-md border border-[#14f1d9] px-3 text-sm font-semibold text-[#14f1d9] hover:bg-[#102018]">Update / close</button>
                </div>
              </form>
              <form action={deleteLocationCapacityAction} className="mt-3 flex flex-wrap justify-end gap-2">
                <input type="hidden" name="location_id" value={location.location_id} />
                <input type="hidden" name="capacity_record_id" value={record.capacity_record_id} />
                <input type="hidden" name="row_version" value={record.row_version} />
                <input type="hidden" name="idempotency_key" value={randomUUID()} />
                <input type="hidden" name="return_to" value={`/locations?location_id=${encodeURIComponent(location.location_id)}`} />
                <input name="reason" required placeholder="delete reason" className="h-9 w-44 rounded-md border border-[#334155] bg-[#0f1115] px-2 text-xs text-white outline-none focus:border-[#14f1d9]" />
                <ConfirmSubmitButton message="Hard-delete this capacity record? Audit evidence is kept." className="h-9 rounded-md border border-[#7f1d1d] px-2 text-xs font-semibold text-[#fecaca]">
                  Delete
                </ConfirmSubmitButton>
              </form>
            </div>
          ))
        )}
      </div>
    </Panel>
  );
}

function ChildrenPanel({ childLocations }: { childLocations: LocationRow[] }) {
  return (
    <Panel title="Children" description={`${childLocations.length.toLocaleString("en-IN")} direct children`}>
      {childLocations.length === 0 ? (
        <EmptyPanel message="No child locations returned." />
      ) : (
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          {childLocations.map((child) => (
            <Link key={child.location_id} href={`/locations?location_id=${encodeURIComponent(child.location_id)}`} className="rounded-md border border-[#334155] bg-[#10141b] p-3 hover:border-[#14f1d9]/60">
              <div className="font-semibold text-white">{child.name}</div>
              <div className="mt-1 text-xs text-[#93a4b8]">{joinParts([formatLabel(child.location_type), child.location_code, child.status])}</div>
            </Link>
          ))}
        </div>
      )}
    </Panel>
  );
}

function ReviewStatCard({
  reviewItems,
  unusable,
  searchParams,
}: {
  reviewItems: ReviewRow[];
  unusable: number;
  searchParams: RouteSearchParams;
}) {
  const href = reviewItems.length > 0 ? reviewHref(searchParams, reviewItems[0].review_id) : null;
  const body = (
    <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-5 transition-colors hover:border-[#a16207]">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className="text-[10px] font-medium uppercase tracking-wider text-[#fb923c]">Review queue</p>
          <p className="mt-1.5 text-2xl font-bold text-white">{reviewItems.length.toLocaleString("en-IN")}</p>
          <p className="mt-1 text-xs text-[#8899AA]">{unusable.toLocaleString("en-IN")} excluded from counts</p>
        </div>
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-[#22262E] text-[#fb923c]">
          <AlertTriangle className="h-4 w-4" aria-hidden="true" />
        </div>
      </div>
    </div>
  );
  return href ? (
    <Link href={href} scroll={false} className="block outline-none focus-visible:ring-2 focus-visible:ring-[#14f1d9]">
      {body}
    </Link>
  ) : (
    body
  );
}

function ReviewQueueLauncher({
  reviewItems,
  reviewError,
  searchParams,
}: {
  reviewItems: ReviewRow[];
  reviewError: ApiUiError | null;
  searchParams: RouteSearchParams;
}) {
  return (
    <Panel title="Review Queue" description="Resolve unknown aliases and alias conflicts in a popup.">
      {reviewError ? (
        <ErrorPanel error={reviewError} />
      ) : reviewItems.length === 0 ? (
        <EmptyPanel message="No open location review items." />
      ) : (
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div className="text-lg font-semibold text-white">{reviewItems.length.toLocaleString("en-IN")} open rows</div>
            <div className="mt-1 text-sm text-[#93a4b8]">
              {reviewItems.map((item) => formatLabel(item.review_type)).join(", ")}
            </div>
          </div>
          <Link
            href={reviewHref(searchParams, reviewItems[0].review_id)}
            scroll={false}
            className="inline-flex h-10 items-center rounded-md border border-[#14f1d9] px-3 text-sm font-semibold text-[#14f1d9] hover:bg-[#102018]"
          >
            Open queue
          </Link>
        </div>
      )}
    </Panel>
  );
}

function ReviewQueueModal({
  reviewItems,
  selectedReview,
  reviewError,
  locations,
  searchParams,
  closeHref,
}: {
  reviewItems: ReviewRow[];
  selectedReview: ReviewRow | null;
  reviewError: ApiUiError | null;
  locations: LocationRow[];
  searchParams: RouteSearchParams;
  closeHref: string;
}) {
  return (
    <Panel
      title="Location Review Queue"
      description={reviewItems.length > 0 ? `${reviewItems.length.toLocaleString("en-IN")} open rows` : "No open rows"}
      action={
        <Link href={closeHref} scroll={false} className="text-sm font-semibold text-[#14f1d9] hover:text-white">
          Close
        </Link>
      }
    >
      {reviewError ? (
        <ErrorPanel error={reviewError} />
      ) : reviewItems.length === 0 ? (
        <EmptyPanel message="No open location review items." />
      ) : (
        <div className="grid gap-4 lg:grid-cols-[280px_minmax(0,1fr)]">
          <div className="space-y-2">
            {reviewItems.map((item) => {
              const selected = selectedReview?.review_id === item.review_id;
              return (
                <Link
                  key={item.review_id}
                  href={reviewHref(searchParams, item.review_id)}
                  scroll={false}
                  aria-current={selected ? "page" : undefined}
                  className={
                    selected
                      ? "block rounded-lg border border-[#14f1d9]/60 bg-[#10141b] p-3 outline-none focus-visible:ring-2 focus-visible:ring-[#14f1d9]"
                      : "block rounded-lg border border-[#334155] bg-[#11151C] p-3 outline-none hover:border-[#14f1d9]/50 focus-visible:ring-2 focus-visible:ring-[#14f1d9]"
                  }
                >
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="truncate font-semibold text-white">{formatLabel(item.review_type)}</div>
                      <div className="mt-1 truncate text-sm text-[#B0BEC5]">{dash(item.normalized_source_label ?? item.source_label)}</div>
                    </div>
                    <span className="shrink-0 rounded border border-[#334155] px-2 py-1 text-[10px] uppercase text-[#93a4b8]">{item.status}</span>
                  </div>
                  <div className="mt-2 text-xs text-[#8899AA]">{item.candidate_location_ids.length.toLocaleString("en-IN")} candidates</div>
                </Link>
              );
            })}
          </div>

          {selectedReview ? (
            <div className="rounded-lg border border-[#334155] bg-[#11151C] p-4">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <div className="text-lg font-semibold text-white">{formatLabel(selectedReview.review_type)}</div>
                  <div className="mt-1 text-sm text-[#B0BEC5]">{dash(selectedReview.normalized_source_label ?? selectedReview.source_label)}</div>
                </div>
                <span className="rounded border border-[#334155] px-2 py-1 text-[10px] uppercase text-[#93a4b8]">{selectedReview.status}</span>
              </div>
              <div className="mt-4 grid grid-cols-1 gap-2 text-sm text-[#8899AA] sm:grid-cols-2">
                <span>{dash(selectedReview.source_context)}</span>
                <span>{selectedReview.candidate_location_ids.length.toLocaleString("en-IN")} candidates</span>
                <span>{shortId(selectedReview.review_id)}</span>
                <span>{dateTime(selectedReview.updated_at)}</span>
              </div>
              <form action={resolveLocationReviewItemAction} className="mt-4 grid grid-cols-1 gap-3 rounded-md border border-[#334155] bg-[#0f1115] p-3">
                <input type="hidden" name="review_id" value={selectedReview.review_id} />
                <input type="hidden" name="row_version" value={selectedReview.row_version} />
                <input type="hidden" name="idempotency_key" value={randomUUID()} />
                <input type="hidden" name="return_to" value={closeHref} />
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <FormSelect name="status" label="Decision" options={["resolved", "dismissed"]} required emptyLabel="Select" />
                  <LocationSelect name="canonical_location_id" label="Canonical location" locations={locations} defaultValue={selectedReview.canonical_location_id ?? ""} />
                </div>
                <FormTextArea name="resolution_notes" label="Notes" required rows={3} />
                <div className="flex justify-end">
                  <ConfirmSubmitButton message="Resolve this location review item?" className="h-10 rounded-md border border-[#14f1d9] px-3 text-sm font-semibold text-[#14f1d9] hover:bg-[#102018]">
                    Save decision
                  </ConfirmSubmitButton>
                </div>
              </form>
            </div>
          ) : (
            <EmptyPanel message="Select a review row." />
          )}
        </div>
      )}
    </Panel>
  );
}

function UsageSummary({ usage }: { usage: LocationUsageResponse }) {
  return (
    <ValueList
      values={[
        ["current goats", usage.goats_currently_assigned.toLocaleString("en-IN")],
        ["history rows", usage.goat_location_history_rows.toLocaleString("en-IN")],
        ["children", usage.child_locations.toLocaleString("en-IN")],
        ["active aliases", usage.active_aliases.toLocaleString("en-IN")],
        ["RBAC grants", usage.active_rbac_grants.toLocaleString("en-IN")],
        ["SOP deps", usage.active_sop_dependencies.toLocaleString("en-IN")],
        ["source rows", usage.import_or_source_rows.toLocaleString("en-IN")],
        ["projection rows", usage.dashboard_projection_rows.toLocaleString("en-IN")],
      ]}
    />
  );
}

function LocationSelect({ name, label, locations, defaultValue = "" }: { name: string; label: string; locations: LocationRow[]; defaultValue?: string }) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <select
        name={name}
        defaultValue={defaultValue}
        className="mt-1 h-10 w-full min-w-0 rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      >
        <option value="">None</option>
        {locations.map((location) => (
          <option key={location.location_id} value={location.location_id}>
            {location.name} · {formatLabel(location.location_type)}
          </option>
        ))}
      </select>
    </label>
  );
}

function OperationalFormFields({ location }: { location?: LocationRow }) {
  const op = location?.operational;
  return (
    <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
      <FlagCheckbox name="usable_for_counts" label="Counts" defaultChecked={op?.usable_for_counts ?? true} />
      <FlagCheckbox name="usable_for_feed" label="Feed" defaultChecked={op?.usable_for_feed ?? true} />
      <FlagCheckbox name="usable_for_vaccination" label="Vaccination" defaultChecked={op?.usable_for_vaccination ?? true} />
      <FlagCheckbox name="usable_for_sop" label="SOP" defaultChecked={op?.usable_for_sop ?? true} />
      <FlagCheckbox name="is_holding" label="Holding" defaultChecked={op?.is_holding ?? false} />
      <FlagCheckbox name="is_quarantine" label="Quarantine" defaultChecked={op?.is_quarantine ?? false} />
      <FlagCheckbox name="is_icu" label="ICU" defaultChecked={op?.is_icu ?? false} />
      <FormField name="display_order" label="Display order" type="number" defaultValue={String(op?.display_order ?? 0)} />
      <div className="sm:col-span-2">
        <FormTextArea name="operational_notes" label="Operational notes" defaultValue={op?.notes ?? ""} rows={2} />
      </div>
    </div>
  );
}

function locationHref(params: RouteSearchParams, locationID?: string): string {
  const next = baseLocationParams(params);
  next.delete("review_id");
  if (locationID) next.set("location_id", locationID);
  const qs = next.toString();
  return qs ? `/locations?${qs}` : "/locations";
}

function createLocationHref(params: RouteSearchParams): string {
  const next = baseLocationParams(params);
  next.delete("location_id");
  next.delete("review_id");
  next.set("create", "location");
  const qs = next.toString();
  return qs ? `/locations?${qs}` : "/locations?create=location";
}

function reviewHref(params: RouteSearchParams, reviewID: string): string {
  const next = baseLocationParams(params);
  next.set("review_id", reviewID);
  const qs = next.toString();
  return qs ? `/locations?${qs}` : "/locations";
}

function locationPageHref(params: RouteSearchParams, page: number, limit: number): string {
  const next = new URLSearchParams();
  copyLocationParam(params, next, "search");
  copyLocationParam(params, next, "type");
  copyLocationParam(params, next, "status");
  next.set("limit", String(limit));
  if (page > 1) next.set("page", String(page));
  const qs = next.toString();
  return qs ? `/locations?${qs}` : "/locations";
}

function baseLocationParams(params: RouteSearchParams): URLSearchParams {
  const next = new URLSearchParams();
  copyLocationParam(params, next, "search");
  copyLocationParam(params, next, "type");
  copyLocationParam(params, next, "status");
  copyLocationParam(params, next, "limit");
  copyLocationParam(params, next, "page");
  copyLocationParam(params, next, "location_id");
  return next;
}

function copyLocationParam(source: RouteSearchParams, target: URLSearchParams, key: string) {
  const value = one(source, key);
  if (value) target.set(key, value);
}

function FlagCheckbox({ name, label, defaultChecked }: { name: string; label: string; defaultChecked: boolean }) {
  return (
    <label className="flex min-h-10 items-center gap-2 rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-[#c7d1dc]">
      <input name={name} type="checkbox" defaultChecked={defaultChecked} className="h-4 w-4 accent-[#14f1d9]" />
      {label}
    </label>
  );
}

function StatCard({
  label,
  value,
  detail,
  icon,
  tone = "neutral",
}: {
  label: string;
  value: number;
  detail: string;
  icon: React.ReactNode;
  tone?: "neutral" | "good" | "warn";
}) {
  const toneClass = tone === "good" ? "text-[#4ade80]" : tone === "warn" ? "text-[#fb923c]" : "text-[#14F1D9]";
  return (
    <div className="rounded-xl border border-[#334155] bg-[#1A1D24] p-5">
      <div className="flex items-start justify-between gap-3">
        <div>
          <p className={`text-[10px] font-medium uppercase tracking-wider ${toneClass}`}>{label}</p>
          <p className="mt-1.5 text-2xl font-bold text-white">{value.toLocaleString("en-IN")}</p>
          <p className="mt-1 text-xs text-[#8899AA]">{detail}</p>
        </div>
        <div className={`flex h-9 w-9 items-center justify-center rounded-lg bg-[#22262E] ${toneClass}`}>{icon}</div>
      </div>
    </div>
  );
}

function OperationalFlags({ location }: { location: LocationRow }) {
  const flags = [
    ["counts", location.operational.usable_for_counts],
    ["feed", location.operational.usable_for_feed],
    ["vaccination", location.operational.usable_for_vaccination],
    ["sop", location.operational.usable_for_sop],
  ] as const;
  return (
    <div className="flex min-w-0 flex-wrap justify-end gap-1">
      {flags.map(([label, enabled]) => (
        <span
          key={label}
          className={
            enabled
              ? "inline-flex items-center gap-1 rounded border border-[#1f8f65] px-2 py-1 text-[10px] uppercase text-[#7dd3a7]"
              : "inline-flex items-center gap-1 rounded border border-[#7f1d1d] px-2 py-1 text-[10px] uppercase text-[#fca5a5]"
          }
        >
          {enabled ? <CheckCircle2 className="h-3 w-3" aria-hidden="true" /> : <AlertTriangle className="h-3 w-3" aria-hidden="true" />}
          {label}
        </span>
      ))}
    </div>
  );
}

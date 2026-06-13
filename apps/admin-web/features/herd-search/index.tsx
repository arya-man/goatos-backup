import Link from "next/link";
import { Search } from "lucide-react";
import { redirect } from "next/navigation";
import { EmptyPanel, ErrorPanel, Mono, NextPageLink, PageHeader, Panel, RowsPerPageSelect } from "@/components/admin-primitives";
import { dash, joinParts, shortId } from "@/lib/format";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { firstAuthRequiredError, searchGoats, type IdentifierType } from "@/lib/api/server";

const identifierTypes: IdentifierType[] = [
  "old_tag",
  "rfid",
  "visual_tag",
  "sheet_row_id",
  "purchase_load_id",
  "temp_field_id",
  "external_system_id",
];

export async function HerdSearchPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const limit = boundedInt(one(searchParams, "limit"), 25, 1, 100);
  const page = boundedInt(one(searchParams, "page"), 1, 1, 1000000);
  const identifierType = normalizeIdentifierType(one(searchParams, "identifier_type"));
  const result = await searchGoats({
    limit,
    cursor: one(searchParams, "cursor"),
    q: one(searchParams, "q"),
    identifier_type: identifierType,
    scope_key: one(searchParams, "scope_key"),
    farm_id: one(searchParams, "farm_id"),
    park_id: one(searchParams, "park_id"),
    location_id: one(searchParams, "location_id"),
    status: one(searchParams, "status"),
  });
  const authError = firstAuthRequiredError(result);
  if (authError) {
    redirect("/login");
  }

  return (
    <>
      <PageHeader
        eyebrow="Herd"
        title="Search Goats"
        description="Search goat passports by display ID, identifier, location, and status."
      />
      <Panel title="Filters" description="Searches stay bounded for fast review.">
        <form className="grid gap-3 md:grid-cols-4 xl:grid-cols-8" action="/herd">
          <Field name="q" label="Search" defaultValue={one(searchParams, "q")} placeholder="display, tag, RFID" wide />
          <Select name="identifier_type" label="Identifier" defaultValue={identifierType ?? ""} options={identifierTypes} />
          <Field name="scope_key" label="Scope" defaultValue={one(searchParams, "scope_key")} />
          <Field name="farm_id" label="Farm ID" defaultValue={one(searchParams, "farm_id")} />
          <Field name="park_id" label="Park ID" defaultValue={one(searchParams, "park_id")} />
          <Field name="location_id" label="Location ID" defaultValue={one(searchParams, "location_id")} />
          <Field name="status" label="Status" defaultValue={one(searchParams, "status")} />
          <RowsPerPageSelect defaultValue={String(limit)} />
          <div className="md:col-span-4 xl:col-span-8">
            <button className="inline-flex h-10 items-center gap-2 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#7ff7ea]">
              <Search className="h-4 w-4" aria-hidden="true" />
              Search
            </button>
          </div>
        </form>
      </Panel>

      <div className="mt-5">
        {!result.ok ? (
          <ErrorPanel error={result.error} />
        ) : (
          <Panel
            title="Results"
            description={
              result.data.items.length > 0
                ? `Showing ${(page - 1) * limit + 1}-${(page - 1) * limit + result.data.items.length} for the current filters.`
                : "No rows returned for the current filters."
            }
          >
            {result.data.items.length === 0 ? (
              <EmptyPanel message="No goats matched these filters." />
            ) : (
              <div className="space-y-3">
                {result.data.items.map((goat) => (
                  <Link
                    key={goat.goat_id}
                    href={`/goats/${goat.goat_id}`}
                    className="block rounded-md border border-[#293241] bg-[#10141b] p-4 hover:border-[#14f1d9]/60"
                  >
                    <div className="grid gap-4 lg:grid-cols-[minmax(360px,1.4fr)_minmax(190px,0.75fr)_minmax(210px,0.8fr)_minmax(120px,auto)] lg:items-center">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-lg font-semibold text-white">{goat.display_id}</span>
                          <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{goat.identity_state}</span>
                          {goat.warnings.length > 0 ? <span className="rounded border border-[#a16207] px-2 py-1 text-xs text-[#facc15]">warnings {goat.warnings.length}</span> : null}
                        </div>
                        <div className="mt-2 flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-sm text-[#aab7c4]">
                          <span className="min-w-0 break-words">RFID {dash(goat.rfid)}</span>
                          <span className="min-w-0 break-words">Old tag {dash(goat.primary_old_tag)}</span>
                        </div>
                      </div>
                      <FieldValue label="Breed / sex / status" value={joinParts([goat.breed, goat.sex, goat.lifecycle_status])} />
                      <FieldValue label="Location" value={dash(goat.location_path.display)} />
                      <div className="min-w-0 lg:text-right">
                        <div className="text-[10px] font-semibold uppercase tracking-wide text-[#94a3b8]">Goat ID</div>
                        <Mono>{shortId(goat.goat_id)}</Mono>
                      </div>
                    </div>
                  </Link>
                ))}
                <NextPageLink
                  href={hrefWithCursor("/herd", searchParams, result.data.next_cursor)}
                  previousHref={hrefPreviousCursor("/herd", searchParams)}
                  currentPage={page}
                  pageSize={limit}
                  itemCount={result.data.items.length}
                />
              </div>
            )}
          </Panel>
        )}
      </div>
    </>
  );
}

function FieldValue({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] font-semibold uppercase tracking-wide text-[#94a3b8]">{label}</div>
      <div className="mt-1 break-words text-sm text-[#c7d1dc]">{value}</div>
    </div>
  );
}

function Field({
  name,
  label,
  defaultValue,
  type = "text",
  placeholder,
  wide = false,
  ...rest
}: {
  name: string;
  label: string;
  defaultValue?: string;
  type?: string;
  placeholder?: string;
  wide?: boolean;
  min?: string;
  max?: string;
}) {
  return (
    <label className={`block ${wide ? "md:col-span-2" : ""}`}>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <input
        name={name}
        type={type}
        defaultValue={defaultValue}
        placeholder={placeholder}
        className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
        {...rest}
      />
    </label>
  );
}

function Select({ name, label, defaultValue, options }: { name: string; label: string; defaultValue: string; options: string[] }) {
  return (
    <label className="block">
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <select
        name={name}
        defaultValue={defaultValue}
        className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      >
        <option value="">Any</option>
        {options.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  );
}

function normalizeIdentifierType(value: string | undefined): IdentifierType | undefined {
  return identifierTypes.includes(value as IdentifierType) ? (value as IdentifierType) : undefined;
}

import Link from "next/link";
import { Search } from "lucide-react";
import { EmptyPanel, ErrorPanel, Mono, NextPageLink, PageHeader, Panel, StatPill } from "@/components/admin-primitives";
import { dateTime, dash, joinParts, shortId } from "@/lib/format";
import { boundedInt, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { searchGoats, type IdentifierType } from "@/lib/api/server";

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
          <Field name="limit" label="Limit" defaultValue={String(limit)} type="number" min="1" max="100" />
          <div className="md:col-span-4 xl:col-span-8">
            <button className="inline-flex h-9 items-center gap-2 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#7ff7ea]">
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
            action={<StatPill label="Page rows" value={result.data.items.length} tone={result.data.items.length > 0 ? "good" : "neutral"} />}
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
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                      <div>
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-lg font-semibold text-white">{goat.display_id}</span>
                          <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{goat.identity_state}</span>
                          {goat.warnings.length > 0 ? <span className="rounded border border-[#a16207] px-2 py-1 text-xs text-[#facc15]">warnings {goat.warnings.length}</span> : null}
                        </div>
                        <div className="mt-2 grid gap-1 text-sm text-[#aab7c4] sm:grid-cols-2 xl:grid-cols-4">
                          <span>RFID {dash(goat.rfid)}</span>
                          <span>Old tag {dash(goat.primary_old_tag)}</span>
                          <span>{joinParts([goat.breed, goat.sex, goat.lifecycle_status])}</span>
                          <span>{dash(goat.location_path.display)}</span>
                        </div>
                      </div>
                      <Mono>{shortId(goat.goat_id)}</Mono>
                    </div>
                  </Link>
                ))}
                <NextPageLink href={hrefWithCursor("/herd", searchParams, result.data.next_cursor)} />
                <div className="text-xs text-[#93a4b8]">Trace {result.data.trace_id}. Rendered {dateTime(new Date().toISOString())}.</div>
              </div>
            )}
          </Panel>
        )}
      </div>
    </>
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
        className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
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
        className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
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

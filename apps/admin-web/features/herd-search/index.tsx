import Link from "next/link";
import { Search } from "lucide-react";
import { redirect } from "next/navigation";
import { EmptyPanel, ErrorPanel, Mono, NextPageLink, PageHeader, Panel, RowsPerPageSelect } from "@/components/admin-primitives";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { dash, shortId } from "@/lib/format";
import { formAction } from "@/lib/routes";
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

const sexOptions = ["male", "female", "unknown"];
const lifecycleStatusOptions = ["alive", "dead", "sold"];

export async function HerdSearchPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const limit = boundedInt(one(searchParams, "limit"), 25, 1, 100);
  const page = boundedInt(one(searchParams, "page"), 1, 1, 1000000);
  const identifierType = normalizeIdentifierType(one(searchParams, "identifier_type"));
  const sex = normalizeSex(one(searchParams, "sex"));
  const lifecycleStatus = normalizeLifecycleStatus(one(searchParams, "status"));
  const result = await searchGoats({
    limit,
    cursor: one(searchParams, "cursor"),
    q: one(searchParams, "q"),
    goat_id: one(searchParams, "goat_id"),
    identifier_type: identifierType,
    scope_key: one(searchParams, "scope_key"),
    breed: one(searchParams, "breed"),
    sex,
    farm_id: one(searchParams, "farm_id"),
    park_id: one(searchParams, "park_id"),
    location_id: one(searchParams, "location_id"),
    status: lifecycleStatus,
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
        description="Search goat passports by display ID, RFID, old tag, goat ID, breed, sex, lifecycle status, and location scope."
      />
      <Panel title="Filters" description="Searches stay bounded for fast review.">
        <form className="grid gap-3 md:grid-cols-4 xl:grid-cols-8" action={formAction("/herd")}>
          <Field name="q" label="Display / tag / RFID" defaultValue={one(searchParams, "q")} placeholder="G-000001, RFID, old tag" wide />
          <Field name="goat_id" label="Goat ID" defaultValue={one(searchParams, "goat_id")} placeholder="UUID" />
          <Select name="identifier_type" label="Identifier" defaultValue={identifierType ?? ""} options={identifierTypes} />
          <Field name="breed" label="Breed" defaultValue={one(searchParams, "breed")} placeholder="Sojat" />
          <Select name="sex" label="Sex" defaultValue={sex ?? ""} options={sexOptions} />
          <Select name="status" label="Lifecycle status" defaultValue={lifecycleStatus ?? ""} options={lifecycleStatusOptions} />
          <Field name="scope_key" label="Scope" defaultValue={one(searchParams, "scope_key")} placeholder="identifier scope" />
          <Field name="farm_id" label="Farm ID" defaultValue={one(searchParams, "farm_id")} />
          <Field name="park_id" label="Park ID" defaultValue={one(searchParams, "park_id")} />
          <Field name="location_id" label="Location ID" defaultValue={one(searchParams, "location_id")} />
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
                <Table className="min-w-[1120px]">
                  <TableHeader>
                    <TableRow className="border-[#293241] hover:bg-transparent">
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">Display ID</TableHead>
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">RFID</TableHead>
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">Old Tag</TableHead>
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">Breed</TableHead>
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">Sex</TableHead>
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">Status</TableHead>
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">Location</TableHead>
                      <TableHead className="px-4 text-[11px] uppercase tracking-wide text-[#94a3b8]">Warnings</TableHead>
                      <TableHead className="px-4 text-right text-[11px] uppercase tracking-wide text-[#94a3b8]">Goat ID</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {result.data.items.map((goat) => (
                      <TableRow key={goat.goat_id} className="border-[#293241] hover:bg-[#121923]">
                        <TableCell className="px-4 py-3">
                          <Link
                            href={`/goats/${goat.goat_id}`}
                            className="-my-2 inline-flex min-h-10 items-center font-semibold text-white hover:text-[#14f1d9]"
                          >
                            {goat.display_id}
                          </Link>
                        </TableCell>
                        <TableCell className="max-w-[210px] px-4 py-3 text-[#c7d1dc]">
                          <span className="block truncate">{dash(goat.rfid)}</span>
                        </TableCell>
                        <TableCell className="max-w-[150px] px-4 py-3 text-[#c7d1dc]">
                          <span className="block truncate">{dash(goat.primary_old_tag)}</span>
                        </TableCell>
                        <TableCell className="max-w-[180px] px-4 py-3 text-[#c7d1dc]">
                          <span className="block truncate">{dash(goat.breed)}</span>
                        </TableCell>
                        <TableCell className="px-4 py-3 capitalize text-[#c7d1dc]">{dash(goat.sex)}</TableCell>
                        <TableCell className="px-4 py-3 text-[#c7d1dc]">{dash(goat.lifecycle_status)}</TableCell>
                        <TableCell className="max-w-[220px] px-4 py-3 text-[#c7d1dc]">
                          <span className="block truncate">{dash(goat.location_path.display)}</span>
                        </TableCell>
                        <TableCell className="px-4 py-3 text-[#c7d1dc]">
                          {goat.warnings.length > 0 ? (
                            <span className="rounded border border-[#a16207] px-2 py-1 text-xs text-[#facc15]">{goat.warnings.length}</span>
                          ) : (
                            <span className="text-[#64748b]">—</span>
                          )}
                        </TableCell>
                        <TableCell className="px-4 py-3 text-right">
                          <Mono>{shortId(goat.goat_id)}</Mono>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
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

function normalizeSex(value: string | undefined): string | undefined {
  return sexOptions.includes(value ?? "") ? value : undefined;
}

function normalizeLifecycleStatus(value: string | undefined): string | undefined {
  return lifecycleStatusOptions.includes(value ?? "") ? value : undefined;
}

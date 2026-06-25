import Link from "next/link";
import { redirect } from "next/navigation";
import { ArrowRight, ChevronLeft, ChevronRight, Plus, Search, Upload } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { dash } from "@/lib/format";
import {
  firstAuthRequiredError,
  searchGoats,
  type GoatSearchResponse,
} from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { backendScope, parseScope } from "@/lib/scope";
import {
  boundedInt,
  hrefPreviousCursor,
  hrefWithCursor,
  one,
  type RouteSearchParams,
} from "@/lib/search-params";

// Counts -> Herd Register. The vaccination cascade's real business entry point: register/import a goat,
// emit goat.created, generate vaccination obligations. This screen is the OPERATIONAL Counts module surface.
//
// Generated-client status (parallel build contract): goat READ and WRITE operation IDs are now present.
// The herd table + filters are real. Register/import remain disabled only until this page wires the
// drawer/form mutations to the generated client; no hand-rolled DTOs, no fake rows. New report has no API.

const PAGE_SIZE = 50;

type GoatRow = GoatSearchResponse["items"][number];

const WRITE_PENDING =
  "Generated write client exists; register/import drawer wiring is still pending for this page.";
const REPORT_PENDING = "No herd report API exists in this slice. New report stays disabled.";

const COLS = ["Goat ID", "Location", "Breed", "Sex", "Lifecycle", "Health", "Breeding"];

function identityTone(state: GoatRow["identity_state"]): "ok" | "warn" | "dng" | "mut" {
  switch (state) {
    case "clean":
      return "ok";
    case "needs_review":
    case "disputed":
      return "warn";
    case "merged":
    case "inactive":
      return "dng";
    default:
      return "mut";
  }
}

export async function HerdRegisterPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const pathname = "/counts/herd";

  // Top bar owns park/as-of scope. parseScope is the single sanctioned reader; backendScope maps it to the
  // params the API actually honors. searchGoats honors park_id; it has no as_of param, so as_of is preserved
  // in the URL for the top bar but not sent here.
  const scope = parseScope(sp);
  const { parkId } = backendScope(scope);

  const q = one(sp, "q");
  const breed = one(sp, "breed");
  const sex = one(sp, "sex");
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const hasFilter = Boolean(q || breed || sex);

  const result = await searchGoats({
    limit: PAGE_SIZE,
    cursor,
    q,
    breed,
    sex,
    park_id: parkId,
  });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const goats: GoatRow[] = result.ok ? result.data.items : [];
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            Counts / <b>Herd</b>
          </div>
          <h1>Herd &amp; Lifecycle</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <a href="#herd-filters" className="btn">
          <Search className="ic" style={{ width: 14 }} aria-hidden="true" />
          Filters
        </a>
        <button
          type="button"
          className="btn"
          disabled
          aria-disabled="true"
          title={WRITE_PENDING}
          style={{ opacity: 0.5, cursor: "not-allowed" }}
        >
          <Upload className="ic" style={{ width: 13 }} aria-hidden="true" />
          Import sheet
        </button>
        {/* Mock shows Register goat as the primary green CTA; it is not wired to a drawer/mutation yet, so
            render it unmistakably disabled with a reason. Restore `btn p` once the interaction is live. */}
        <button
          type="button"
          className="btn"
          disabled
          aria-disabled="true"
          title={WRITE_PENDING}
          style={{ opacity: 0.5, cursor: "not-allowed" }}
        >
          <Plus className="ic" aria-hidden="true" />
          Register goat
        </button>
        <button
          type="button"
          className="btn"
          disabled
          aria-disabled="true"
          title={REPORT_PENDING}
          style={{ opacity: 0.5, cursor: "not-allowed" }}
        >
          New report
        </button>
      </div>

      {/* Census KPIs need a counts read-model (a total/aggregate endpoint). /goats/search returns a page of
          rows + a cursor, never a herd total — so showing a number here would be faked. The cards keep the
          mock's structure with an honest pending value; the live table below is the real data. */}
      <div className="grid g4" style={{ marginBottom: 8 }}>
        {["Active", "Adults", "Kids", "Untagged kids"].map((label) => (
          <div key={label} className="kpi" title="Herd census totals need a Counts aggregate read-model (not in the generated client yet).">
            <div className="lab">{label}</div>
            <div className="val">—</div>
            <div className="dl">
              <span className="muted">count read-model pending</span>
            </div>
          </div>
        ))}
      </div>
      <div className="note" style={{ marginBottom: 16 }}>
        Herd census totals need a Counts aggregate read-model (not in the generated client yet), so they are
        not shown rather than faked. The table below reads live goats from <b>/goats/search</b>.
      </div>

      <form id="herd-filters" method="get" className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <Search className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>Filters</h3>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">server-side · /goats/search</span>
        </div>
        <div className="bd" style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
          {/* Preserve top-bar scope across the GET filter submit so the bar never disagrees with the page. */}
          {scope.mode === "park" && scope.parkId ? (
            <>
              <input type="hidden" name="scope_mode" value="park" />
              <input type="hidden" name="park" value={scope.parkId} />
            </>
          ) : (
            <input type="hidden" name="scope_mode" value="company" />
          )}
          {scope.asOf ? <input type="hidden" name="as_of" value={scope.asOf} /> : null}

          <div className="fld" style={{ flex: 2, minWidth: 200, marginBottom: 0 }}>
            <label htmlFor="herd-q">Search (RFID / old tag / id)</label>
            <input id="herd-q" name="q" defaultValue={q ?? ""} placeholder="e.g. RF-9001 or CB-201" />
          </div>
          <div className="fld" style={{ flex: 1, minWidth: 140, marginBottom: 0 }}>
            <label htmlFor="herd-breed">Breed</label>
            <input id="herd-breed" name="breed" defaultValue={breed ?? ""} placeholder="e.g. Beetal" />
          </div>
          <div className="fld" style={{ width: 120, marginBottom: 0 }}>
            <label htmlFor="herd-sex">Sex</label>
            <input id="herd-sex" name="sex" defaultValue={sex ?? ""} placeholder="F / M" />
          </div>
          <button type="submit" className="btn p">
            Apply
          </button>
          {hasFilter ? (
            <Link href={pathname} className="btn">
              Clear
            </Link>
          ) : null}
        </div>
      </form>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      <section className="card">
        <div className="hd">
          <h3>Herd</h3>
          <Tag tone={goats.length ? "info" : "mut"}>{goats.length}</Tag>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">tap a row → Goat Passport</span>
        </div>
        <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label="Herd">
          <table>
            <thead>
              <tr>
                {COLS.map((c) => (
                  <th key={c}>{c}</th>
                ))}
                <th aria-label="Open" />
              </tr>
            </thead>
            <tbody>
              {goats.length === 0 ? (
                <tr>
                  <td colSpan={COLS.length + 1}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {result.ok
                        ? hasFilter
                          ? "No goats match these filters for this scope."
                          : "No goats for this scope yet. Goats appear here once registered or imported — the create/import write path is pending backend (see the disabled Register goat / Import sheet actions)."
                        : "Herd is unavailable until the goats read API responds."}
                    </div>
                  </td>
                </tr>
              ) : (
                goats.map((g) => {
                  const href = `/goats/${encodeURIComponent(g.goat_id)}`;
                  return (
                    <tr key={g.goat_id}>
                      <td>
                        <Link href={href} className="gid">
                          {g.display_id}
                        </Link>
                        {g.identity_state !== "clean" ? (
                          <>
                            {" "}
                            <Tag tone={identityTone(g.identity_state)} title="Identity state from the goats read model">
                              {g.identity_state.replace("_", " ")}
                            </Tag>
                          </>
                        ) : null}
                      </td>
                      <td className="muted">{dash(g.location_path?.display)}</td>
                      <td>{dash(g.breed)}</td>
                      <td>{dash(g.sex)}</td>
                      <td className="muted">{dash(g.lifecycle_status)}</td>
                      <td className="muted">{dash(g.health_status)}</td>
                      <td className="muted">{dash(g.reproductive_status)}</td>
                      <td>
                        <Link href={href} className="lk small">
                          Passport <ArrowRight className="ic" style={{ width: 13 }} aria-hidden="true" />
                        </Link>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        {goats.length > 0 || page > 1 ? (
          <div className="bd" style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <span className="muted small">
              Page {page} · {goats.length} goat{goats.length === 1 ? "" : "s"} on this page
            </span>
            <div className="sp" style={{ flex: 1 }} />
            {prevHref ? (
              <Link href={prevHref} scroll={false} className="btn sm">
                <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
              </Link>
            ) : (
              <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
                <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
              </span>
            )}
            {nextHref ? (
              <Link href={nextHref} scroll={false} className="btn sm">
                Next <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
              </Link>
            ) : (
              <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
                Next <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
              </span>
            )}
          </div>
        ) : null}
      </section>
    </div>
  );
}

import { listOrEmpty } from "@/lib/list-or-empty";
// The Toxin review screen (maintainer decision 2026-08-25) — the /verify page renders this INSTEAD
// of the verification queue when the backend-declared `toxin_tab` control is enabled AND the
// ?toxin=1 chip is selected. Server component: one keyset page of GET /toxin/review (defaulting to
// pending_review server-side), handed to the client list whose drawer is local overlay state.
//
// CEO/CXO ONLY by construction: the chip that reaches this is gated on controlEnabled(pageContract,
// "toxin_tab", false), and the endpoint independently requires toxin.verdict — the two halves of
// the role-scoped-UI lock (docs/decisions/role-scoped-ui-is-capability-gated.md). Toxin is
// deliberately NOT a verification category; the tenant verifier never sees this screen.

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listToxinReview } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { redirect } from "next/navigation";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { ToxinReviewList } from "./toxin-review-list";
import Alert from "@mui/material/Alert";
import { PageHeader } from "@/components/app/page-header";
import Card from "@mui/material/Card";
import Typography from "@mui/material/Typography";
import { AnimatedTabs } from "@/components/minimal/list/animated-tabs";
import { TablePaginationLinks } from "@/components/minimal/table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { TableSkeleton } from "@/components/app/skeletons";

const PATHNAME = "/verify";

// The params the toxin list reads. A change to any of them swaps ONLY the list to its skeleton
// (UrlSuspense); the header and the tabs stay painted. guard: url-keyed-panel
const TOXIN_WATCH = ["toxin", "tx_cursor", "tx_status", "tx_code"] as const;

export function ToxinReviewScreen({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const toxinLabel = toxinTabLabel(pageContract);

  // The shell renders from the search params alone (no await), so switching to the Toxin tab paints
  // the header + tabs at once and only the list waits on GET /toxin/review behind its skeleton.
  return (
    <div className="screen on">
      <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]} />

      {/* Template list card (InvoiceListView): the Tabs row carries the way back to the verification
          queue and the active Toxin tab, then the table and the pagination footer. */}
      <Card className="vr-board" aria-label={copy(pageContract, "board.title")} sx={{ minWidth: 0 }}>
        <AnimatedTabs
          ariaLabel={toxinLabel}
          value="toxin"
          sx={{ px: { md: 2.5 } }}
          items={[
            { value: "all", label: copy(pageContract, "filter.all_modules"), href: hrefWith(sp, { toxin: null, tx_cursor: null, tx_status: null, tx_code: null }) },
            { value: "toxin", label: toxinLabel },
          ]}
        />

        <UrlSuspense searchParams={sp} watch={TOXIN_WATCH} fallback={<TableSkeleton bare header={false} columns={3} rows={10} />}>
          <ToxinReviewPanel searchParams={sp} pageContract={pageContract} />
        </UrlSuspense>
      </Card>
    </div>
  );
}

async function ToxinReviewPanel({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;
  const cursor = one(sp, "tx_cursor") || undefined;
  // ONE page (~20 rows) per render; the pager walks next_cursor. Never drain the cursor.
  const page = await listToxinReview({ cursor, limit: 20 });
  const authError = firstAuthRequiredError(page);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  const tasks = page.ok ? listOrEmpty(page.data.tasks) : [];
  const nextCursor = page.ok ? page.data.next_cursor : undefined;
  const feedback = { status: one(sp, "tx_status"), code: one(sp, "tx_code") };
  const returnTo = hrefWith(sp, { tx_status: null, tx_code: null });

  return (
    <>
      {page.ok ? null : (
        <Alert severity="error" sx={{ m: 2.5 }}>
          <b>{copy(pageContract, "state.queue_unavailable")}</b>
          <Typography variant="body2" sx={{ mt: 0.5, color: "text.secondary" }}>
            {page.error.code ?? page.error.kind} · {page.error.message}
          </Typography>
        </Alert>
      )}

      <ToxinReviewList tasks={tasks} pageContract={pageContract} returnTo={returnTo} feedback={feedback} />

      {nextCursor || cursor ? (
        // Keyset cursors only read forward; "back" returns to the first page rather than growing a
        // trail — the review backlog is expected to stay shallow.
        <TablePaginationLinks
          className="pager"
          page={cursor ? 1 : 0}
          rowsPerPage={20}
          count={-1}
          replace
          rangeLabel=""
          prevLabel={copy(pageContract, "pagination.previous")}
          nextLabel={copy(pageContract, "pagination.next")}
          prevHref={cursor ? hrefWith(sp, { tx_cursor: null, tx_status: null, tx_code: null }) : null}
          nextHref={nextCursor ? hrefWith(sp, { tx_cursor: nextCursor, tx_status: null, tx_code: null }) : null}
        />
      ) : null}
    </>
  );
}

/**
 * The chip label comes from the backend contract control, falling back to its copy key. Both are
 * backend-owned; there is deliberately no client literal — the chip only renders when the backend
 * declares the control, so a label is always present.
 */
export function toxinTabLabel(pageContract: AdminUiPageContract): string {
  const declared = pageContract.controls.find((item) => item.id === "toxin_tab")?.label;
  return declared && declared.trim() ? declared : copy(pageContract, "toxin_tab.title", "");
}

function hrefWith(params: RouteSearchParams, updates: Record<string, string | null | undefined>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (Array.isArray(value)) {
      for (const item of value) next.append(key, item);
    } else if (value) {
      next.set(key, value);
    }
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null || value === undefined || value === "") next.delete(key);
    else next.set(key, value);
  }
  const qs = next.toString();
  return qs ? `${PATHNAME}?${qs}` : PATHNAME;
}

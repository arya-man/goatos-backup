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
import Link from "@/components/no-prefetch-link";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { firstAuthRequiredError, listToxinReview } from "@/lib/api/server";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { redirect } from "next/navigation";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { ToxinReviewList } from "./toxin-review-list";
import Alert from "@mui/material/Alert";
import { PageHeader } from "@/components/app/page-header";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import { LinkButton } from "@/components/minimal/link-button";

const PATHNAME = "/verify";

export async function ToxinReviewScreen({
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
  const toxinLabel = toxinTabLabel(pageContract);
  const feedback = { status: one(sp, "tx_status"), code: one(sp, "tx_code") };
  const returnTo = hrefWith(sp, { tx_status: null, tx_code: null });

  return (
    <div className="screen on">
      <PageHeader title={pageContract.title} crumbs={[{ label: copy(pageContract, "crumb") }, { label: pageContract.title }]} />

      {page.ok ? null : (
        <Alert severity="error" style={{ marginBottom: 14 }}>
          <b>{copy(pageContract, "state.queue_unavailable")}</b>
          <div className="small muted" style={{ marginTop: 4 }}>
            {page.error.code ?? page.error.kind} · {page.error.message}
          </div>
        </Alert>
      )}

      <Card className="vr-board" sx={{ minWidth: 0 }}>
        <CardHeader title={copy(pageContract, "board.title")} />

        {/* The way back to the verification queue plus the active Toxin chip — the same .vr-lg
            vocabulary as the queue's module chip row, so the toggle reads as one chip family. */}
        <Box className="vr-legend" role="group" aria-label={toxinLabel} sx={{ px: 3, pt: 2 }}>
          <Link href={hrefWith(sp, { toxin: null, tx_cursor: null, tx_status: null, tx_code: null })} replace scroll={false} className="vr-lg">
            {copy(pageContract, "filter.all_modules")}
          </Link>
          <span className="vr-lg on">{toxinLabel}</span>
        </Box>

        <ToxinReviewList tasks={tasks} pageContract={pageContract} returnTo={returnTo} feedback={feedback} />

        {nextCursor || cursor ? (
          <Box className="pager" sx={{ px: 3, py: 2, display: "flex", alignItems: "center", gap: 1.5 }}>
            {cursor ? (
              // Keyset cursors only read forward; "back" returns to the first page rather than
              // growing a trail — the review backlog is expected to stay shallow.
              <LinkButton href={hrefWith(sp, { tx_cursor: null, tx_status: null, tx_code: null })} size="small" variant="outlined" color="inherit" replace scroll={false}>
                {copy(pageContract, "pagination.previous")}
              </LinkButton>
            ) : null}
            {nextCursor ? (
              <LinkButton href={hrefWith(sp, { tx_cursor: nextCursor, tx_status: null, tx_code: null })} size="small" variant="outlined" color="inherit" replace scroll={false}>
                {copy(pageContract, "pagination.next")}
              </LinkButton>
            ) : null}
          </Box>
        ) : null}
      </Card>
    </div>
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

import { Users } from "lucide-react";

import Link from "@/components/no-prefetch-link";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult } from "@/lib/api/server";
import type { MarketReporter } from "@/lib/api/market-server";
import { setMarketReporterAction } from "./market-actions";
import { MarketConfigForm, type MarketActionOutcomes } from "./market-config-form";

/**
 * WHO makes the morning market calls (Sales SOP page, maintainer instruction 2026-09-19): the
 * people holding the market survey phone module, each with a one-tap give/take that writes
 * through the same person-access path /people uses. Everyone with a login is listed, reporters
 * first, so the survey's owner can hand the calls to someone new without leaving the page.
 */
export function MarketReportersSection({
  pageContract,
  result,
  canConfigure,
}: {
  pageContract: AdminUiPageContract;
  result: ApiResult<{ people: MarketReporter[] }>;
  canConfigure: boolean;
}) {
  const outcomes: MarketActionOutcomes = {
    market_reporter_saved: copy(pageContract, "action.market_reporter_saved"),
    market_save_failed: copy(pageContract, "action.market_save_failed"),
  };
  const people = result.ok ? result.data.people : [];
  const reporters = people.filter((p) => p.reporter);
  const others = people.filter((p) => !p.reporter);
  return (
    <section className="card" aria-label={copy(pageContract, "market.reporters.aria")} data-testid="market-reporters">
      <div className="hd">
        <Users className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "market.reporters.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <Link href="/people" className="btn sm">
          {copy(pageContract, "market.reporters.people_link")}
        </Link>
      </div>
      <p className="muted small sales-config-card-copy">{copy(pageContract, "market.reporters.sub")}</p>
      <div className="bd market-config-body">
        {!result.ok ? <div className="alert">{result.error.message || copy(pageContract, "error.load")}</div> : null}
        {!canConfigure ? <div className="note">{copy(pageContract, "disabled.market_config")}</div> : null}
        <h4 className="market-config-h4">
          {copy(pageContract, "market.reporters.current")}
          <span className="market-config-count">{reporters.length}</span>
        </h4>
        {reporters.length === 0 ? <div className="empty">{copy(pageContract, "market.reporters.none")}</div> : null}
        <ul className="market-config-list">
          {reporters.map((p) => (
            <li key={p.person_id}>
              <ReporterRow person={p} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
            </li>
          ))}
        </ul>
        {canConfigure && others.length > 0 ? (
          <details className="market-config-add" style={{ display: "block" }}>
            <summary className="muted small" style={{ cursor: "pointer" }}>
              {copy(pageContract, "market.reporters.add")} ({others.length})
            </summary>
            <ul className="market-config-list">
              {others.map((p) => (
                <li key={p.person_id}>
                  <ReporterRow person={p} pageContract={pageContract} canConfigure={canConfigure} outcomes={outcomes} />
                </li>
              ))}
            </ul>
          </details>
        ) : null}
      </div>
    </section>
  );
}

function ReporterRow({
  person,
  pageContract,
  canConfigure,
  outcomes,
}: {
  person: MarketReporter;
  pageContract: AdminUiPageContract;
  canConfigure: boolean;
  outcomes: MarketActionOutcomes;
}) {
  const line = [person.title, person.park_label].filter(Boolean).join(" · ");
  if (!canConfigure) {
    return (
      <div className="market-config-line">
        <span style={{ flex: 1 }}>
          {person.display_name}
          {line ? <span className="muted small"> · {line}</span> : null}
        </span>
        {person.reporter ? <Tag tone="ok">{copy(pageContract, "market.reporters.tag")}</Tag> : null}
      </div>
    );
  }
  return (
    <MarketConfigForm action={setMarketReporterAction} outcomes={outcomes} className="market-config-row" data-market-reporter={person.person_id}>
      <input type="hidden" name="person_id" value={person.person_id} />
      <input type="hidden" name="enabled" value={person.reporter ? "false" : "true"} />
      <span style={{ flex: 1 }}>
        {person.display_name}
        {line ? <span className="muted small"> · {line}</span> : null}
      </span>
      {person.reporter ? <Tag tone="ok">{copy(pageContract, "market.reporters.tag")}</Tag> : null}
      <button type="submit" className="btn sm">
        {person.reporter ? copy(pageContract, "market.reporters.remove") : copy(pageContract, "market.reporters.give")}
      </button>
    </MarketConfigForm>
  );
}

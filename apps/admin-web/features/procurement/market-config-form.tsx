"use client";

// One market-config form (call time, add city, a city row, add question, a question row) that
// posts its Server Action and shows the outcome BESIDE the row it belongs to.
//
// Why a client wrapper at all (maintainer report 2026-09-15): the section's forms used to post an
// action that redirected back to the page with `?notice=`, and a redirect is a full navigation --
// the whole page re-rendered and the reader landed at the top, three cards above the row they had
// just edited. `useActionState` keeps the outcome with the form; the action's own revalidation
// re-reads the section's data in the same response, so the list updates under the reader's eyes
// and the scroll position never moves.
//
// This file composes no copy of its own: every sentence arrives resolved from the page contract.
import { useActionState, type ReactNode } from "react";

import { addMarketCityAction, type MarketActionState } from "./market-actions";

export type MarketActionOutcomes = Record<string, string>;

/** A market-config Server Action; every one in market-actions shares this shape. */
export type MarketAction = typeof addMarketCityAction;

const INITIAL: MarketActionState = { status: "idle", code: "", ticket: 0 };

export function MarketConfigForm({
  action,
  outcomes,
  className,
  style,
  children,
  ...rest
}: {
  action: MarketAction;
  /** Backend sentences by outcome code (`action.<code>`), resolved by the section. */
  outcomes: MarketActionOutcomes;
  className?: string;
  style?: React.CSSProperties;
  children: ReactNode;
} & Record<`data-${string}`, string | undefined>) {
  const [state, formAction, pending] = useActionState(action, INITIAL);
  const message = state.status === "idle" ? "" : outcomes[state.code] || outcomes.market_save_failed || "";
  return (
    <div className={className} style={{ display: "flex", flexDirection: "column", gap: 4 }}>
      {/* Every market form is one horizontal line of controls; the layout lives in .market-config-line. */}
      <form action={formAction} className="market-config-line" style={style} aria-busy={pending} {...rest}>
        {children}
      </form>
      {message ? (
        <div
          className={state.status === "success" ? "market-config-msg ok" : "market-config-msg bad"}
          role="status"
          style={{ fontSize: 12, color: state.status === "success" ? "var(--ok)" : "var(--danger)" }}
        >
          {message}
        </div>
      ) : null}
    </div>
  );
}

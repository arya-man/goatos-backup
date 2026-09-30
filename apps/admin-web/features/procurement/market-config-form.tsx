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
import Box from "@mui/material/Box";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { addMarketCityAction, type MarketActionState } from "./market-actions";

export type MarketActionOutcomes = Record<string, string>;

/** A market-config Server Action; every one in market-actions shares this shape. */
export type MarketAction = typeof addMarketCityAction;

const INITIAL: MarketActionState = { status: "idle", code: "", ticket: 0 };

export function MarketConfigForm({
  action,
  outcomes,
  className,
  children,
  ...rest
}: {
  action: MarketAction;
  /** Backend sentences by outcome code (`action.<code>`), resolved by the section. */
  outcomes: MarketActionOutcomes;
  className?: string;
  children: ReactNode;
} & Record<`data-${string}`, string | undefined>) {
  const [state, formAction, pending] = useActionState(action, INITIAL);
  const message = state.status === "idle" ? "" : outcomes[state.code] || outcomes.market_save_failed || "";
  return (
    <Stack spacing={0.5} className={className} sx={{ minWidth: 0 }}>
      {/* One wrapping row of template fields: each TextField keeps its own min width and the row
          wraps on a phone instead of squeezing a name to "Goat" / "Shee" (FJ3 P1-3). */}
      <Box
        component="form"
        action={formAction}
        aria-busy={pending}
        sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1.5, minWidth: 0 }}
        {...rest}
      >
        {children}
      </Box>
      {message ? (
        <Typography role="status" variant="caption" sx={{ color: state.status === "success" ? "success.main" : "error.main" }}>
          {message}
        </Typography>
      ) : null}
    </Stack>
  );
}

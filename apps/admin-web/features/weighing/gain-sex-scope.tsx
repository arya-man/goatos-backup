"use client";

// Which kids the breed gain card counts, shared between the Sex control in the page's filter bar
// and the card itself.
//
// WHY THIS IS NOT A URL FILTER like Park, Period and Weighing beside it. Those change what the
// SERVER has to fetch, so they must travel in the URL and cost a render. This one does not: the
// backend already sends each breed combined AND per sex in the SAME response, so all three
// answers are in the browser before the reader touches anything. Routing this through the URL
// re-rendered the whole page — the shed table, both leaderboards, the load chart, the losing-kids
// list and every read behind them — to show rows the reader had already been sent. Measured on
// the local stack that was 1.4–5.7s per switch; holding it in client state is ~120ms and issues
// no request at all.
//
// The URL is still kept in step, with `history.replaceState`, so a shared link still carries the
// grain the sender was reading and a reload still lands on it. `replaceState` specifically, not a
// router push: a push is the round trip being avoided, and the grain is a reading preference
// rather than a place, so it does not deserve its own Back entry — Back should leave the page.
//
// The provider wraps the whole page rather than one card because the control and the card it
// governs sit in different parts of the tree: the control is in the filter bar at the top, the
// card is ~2,600px down. A server component cannot hold state to join them, so this client
// provider does, and both server-rendered halves pass through it as children unchanged.
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from "react";

export type GainSex = "all" | "male" | "female";

type GainSexScope = { sex: GainSex; setSex: (next: GainSex) => void };

// Defaults to the combined grain so a consumer rendered outside the provider still shows every
// kid — the same answer the server gives when no sex was asked for — instead of throwing.
const GainSexContext = createContext<GainSexScope>({ sex: "all", setSex: () => {} });

export function useGainSex(): GainSexScope {
  return useContext(GainSexContext);
}

export function GainSexProvider({
  initialSex,
  searchParamName,
  children,
}: {
  /** The grain the server rendered, read from the URL. Combined unless a sex was asked for. */
  initialSex: GainSex;
  /** The search param the grain is mirrored into, so a shared link carries it. */
  searchParamName: string;
  children: ReactNode;
}) {
  const [sex, setSexState] = useState<GainSex>(initialSex);

  const setSex = useCallback(
    (next: GainSex) => {
      setSexState(next);
      if (typeof window === "undefined") return;
      const url = new URL(window.location.href);
      // Combined CLEARS the parameter rather than writing "all": absent already means combined
      // everywhere else — the server's default, and a link made before this control existed —
      // and two spellings of one state would eventually disagree.
      if (next === "all") url.searchParams.delete(searchParamName);
      else url.searchParams.set(searchParamName, next);
      window.history.replaceState(window.history.state, "", `${url.pathname}${url.search}${url.hash}`);
    },
    [searchParamName],
  );

  const value = useMemo(() => ({ sex, setSex }), [sex, setSex]);
  return <GainSexContext.Provider value={value}>{children}</GainSexContext.Provider>;
}

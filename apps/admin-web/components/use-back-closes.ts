"use client";

import { useEffect, useRef } from "react";

const BACK_CLOSES_KEY = "__goatosBackCloses";

function entryIsOurs(): boolean {
  const state = window.history.state;
  return Boolean(state && typeof state === "object" && state[BACK_CLOSES_KEY]);
}

/**
 * Browser Back closes an overlay that lives in component state rather than in the URL.
 *
 * The house drawer rule is that X, Escape, an outside click AND Back all close a drawer. A drawer
 * whose selection is React state (a matrix cell, a tile) has no URL of its own, so Back used to
 * leave the whole page (Command Board, 2026-09-26). While `open` is true this keeps ONE same-URL
 * history entry, marked as ours and carrying Next's own state forward: Back pops it and calls
 * `close`; closing any other way (X, Escape, scrim, switching to another page action) pops the
 * entry it added, so the history never collects a dead step. Switching between drawers while one
 * is open does not push again -- `open` stays true.
 */
export function useBackCloses(open: boolean, close: () => void): void {
  const pushed = useRef(false);
  const closeRef = useRef(close);
  useEffect(() => {
    closeRef.current = close;
  }, [close]);

  useEffect(() => {
    if (open && !pushed.current) {
      const state = window.history.state && typeof window.history.state === "object" ? window.history.state : {};
      window.history.pushState({ ...state, [BACK_CLOSES_KEY]: true }, "", window.location.href);
      pushed.current = true;
    } else if (!open && pushed.current) {
      pushed.current = false;
      if (entryIsOurs()) window.history.back();
    }
  }, [open]);

  useEffect(() => {
    const onPop = () => {
      if (!pushed.current || entryIsOurs()) return;
      pushed.current = false;
      closeRef.current();
    };
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);
}

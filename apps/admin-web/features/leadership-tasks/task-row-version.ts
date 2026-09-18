"use client";

import { useSyncExternalStore } from "react";

/**
 * The task's `row_version` as the browser last learned it.
 *
 * The detail panel is server-rendered, so the status menu and the Edit modal carry the
 * `row_version` the page LOADED with. The comment composer, an island beside them, posts in
 * place -- and every note bumps the row's version on the server. Left alone, the next status
 * change sends the stale version and the backend refuses it as a conflict ("this task was
 * changed while this board was open") for a change the same person just made. The islands do
 * not share React state, so the freshest version lives here: a write publishes it, a fence
 * reads it, and the SSR value is only the starting point.
 */
const versions = new Map<string, number>();
const listeners = new Map<string, Set<() => void>>();

export function publishTaskRowVersion(taskID: string, rowVersion: number): void {
  if (!Number.isFinite(rowVersion)) return;
  const known = versions.get(taskID);
  if (known !== undefined && known >= rowVersion) return;
  versions.set(taskID, rowVersion);
  for (const notify of listeners.get(taskID) ?? []) notify();
}

export function useTaskRowVersion(taskID: string, initial: number): number {
  return useSyncExternalStore(
    (notify) => {
      const set = listeners.get(taskID) ?? new Set();
      set.add(notify);
      listeners.set(taskID, set);
      return () => {
        set.delete(notify);
      };
    },
    () => {
      const known = versions.get(taskID);
      return known !== undefined && known > initial ? known : initial;
    },
    () => initial,
  );
}

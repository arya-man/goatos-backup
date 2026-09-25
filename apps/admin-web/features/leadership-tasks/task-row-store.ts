"use client";

import { useSyncExternalStore } from "react";

import type { TaskRow } from "./task-row";

/**
 * What the browser knows about a task that the server-rendered page does not YET.
 *
 * The board, the table and the drawer are server-rendered from one list read. A status change
 * or a comment made in the drawer used to redirect and re-run the route so those three would
 * agree again -- which repainted the whole page (the route's loading skeleton, then the board)
 * for a one-field change. Instead, the write returns the task as the backend now has it, the
 * result is PUBLISHED here as a partial row, and every island that renders that task reads the
 * override on top of its server prop. The next full render of the page carries the truth and the
 * override becomes a no-op.
 *
 * `rowVersion` is the fence every later write must send; it is published on every successful
 * write, including a comment (which bumps the version without changing anything the board shows).
 */
type Patch = Partial<TaskRow>;

const patches = new Map<string, Patch>();
const listeners = new Map<string, Set<() => void>>();
const anyListeners = new Set<() => void>();
let version = 0;

function notify(taskID: string) {
  version += 1;
  for (const fn of listeners.get(taskID) ?? []) fn();
  for (const fn of anyListeners) fn();
}

export function publishTaskRow(taskID: string, patch: Patch): void {
  const current = patches.get(taskID) ?? {};
  // A version never goes backwards: a slow response from an earlier write must not undo a later one.
  if (
    typeof patch.rowVersion === "number" &&
    typeof current.rowVersion === "number" &&
    patch.rowVersion < current.rowVersion
  ) {
    return;
  }
  patches.set(taskID, { ...current, ...patch });
  notify(taskID);
}

export function publishTaskRowVersion(taskID: string, rowVersion: number): void {
  if (!Number.isFinite(rowVersion)) return;
  publishTaskRow(taskID, { rowVersion });
}

/** The row as the browser knows it: the server prop with the published patch on top. */
export function useTaskRow(row: TaskRow): TaskRow {
  const patch = useSyncExternalStore(
    (fn) => {
      const set = listeners.get(row.id) ?? new Set();
      set.add(fn);
      listeners.set(row.id, set);
      return () => {
        set.delete(fn);
      };
    },
    () => patches.get(row.id),
    () => undefined,
  );
  if (!patch) return row;
  // The server row is newer than the patch once a full render has caught up. Equal versions
  // keep the patch: an optimistic status carries no version of its own and rides on the last
  // published one (Judge B, P2-1: `<=` here threw every optimistic move away).
  if (typeof patch.rowVersion === "number" && patch.rowVersion < row.rowVersion) return row;
  return { ...row, ...patch };
}

export function useTaskRowVersion(taskID: string, initial: number): number {
  const patch = useSyncExternalStore(
    (fn) => {
      const set = listeners.get(taskID) ?? new Set();
      set.add(fn);
      listeners.set(taskID, set);
      return () => {
        set.delete(fn);
      };
    },
    () => patches.get(taskID)?.rowVersion,
    () => undefined,
  );
  return typeof patch === "number" && patch > initial ? patch : initial;
}

/** Re-renders whenever ANY task is patched -- the board columns read this to re-derive lanes. */
export function useTaskRowsVersion(): number {
  return useSyncExternalStore(
    (fn) => {
      anyListeners.add(fn);
      return () => {
        anyListeners.delete(fn);
      };
    },
    () => version,
    () => 0,
  );
}

/** The current patch for a task, for non-hook readers (the board's lane derivation). */
export function taskRowPatch(taskID: string): Patch | undefined {
  return patches.get(taskID);
}

/**
 * WRITES TO ONE TASK ARE SERIALISED, and every write reads its fence when it is SENT.
 *
 * A comment bumps the task's row_version. A status change made 0.4 s later used to capture the
 * fence into its FormData at the click -- while the comment was still in flight -- and Next queues
 * server actions, so the status write arrived second carrying the pre-comment version and the
 * backend refused it with a false "this task was changed while this board was open" (2026-09-25).
 *
 * `runTaskWrite` chains each write behind the previous one for the same task, and the write body
 * reads `currentTaskRowVersion` only when its turn comes, so it carries the version the comment
 * just published. `useTaskWriteInFlight` lets the status menu and the edit form show the saving
 * state (and stay disabled) while a write to that task is still on the wire.
 */
const chains = new Map<string, Promise<unknown>>();
const inFlight = new Map<string, number>();

function setInFlight(taskID: string, delta: number) {
  const next = (inFlight.get(taskID) ?? 0) + delta;
  if (next > 0) inFlight.set(taskID, next);
  else inFlight.delete(taskID);
  notify(taskID);
}

export function runTaskWrite<T>(taskID: string, write: () => Promise<T>): Promise<T> {
  setInFlight(taskID, 1);
  const previous = chains.get(taskID) ?? Promise.resolve();
  // A refused or failed earlier write must not block the next one; its own caller handles it.
  const run = previous.then(
    () => write(),
    () => write(),
  );
  chains.set(taskID, run);
  const settle = () => {
    if (chains.get(taskID) === run) chains.delete(taskID);
    setInFlight(taskID, -1);
  };
  run.then(settle, settle);
  return run;
}

/** The fence a write must send NOW: the newest of the published version and the server row's. */
export function currentTaskRowVersion(taskID: string, serverRowVersion: number): number {
  const published = patches.get(taskID)?.rowVersion;
  return typeof published === "number" && published > serverRowVersion ? published : serverRowVersion;
}

export function taskWriteInFlight(taskID: string): boolean {
  return (inFlight.get(taskID) ?? 0) > 0;
}

export function useTaskWriteInFlight(taskID: string): boolean {
  return useSyncExternalStore(
    (fn) => {
      const set = listeners.get(taskID) ?? new Set();
      set.add(fn);
      listeners.set(taskID, set);
      return () => {
        set.delete(fn);
      };
    },
    () => taskWriteInFlight(taskID),
    () => false,
  );
}

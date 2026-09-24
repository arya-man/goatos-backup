"use client";

import { useSyncExternalStore } from "react";
import type { HerdSignalsLiveResponse } from "@/lib/api/herd-signals";

type LiveSnapshot = {
  key: string;
  data: HerdSignalsLiveResponse;
  receivedAt: number;
};

let snapshot: LiveSnapshot | null = null;
const listeners = new Set<() => void>();

function subscribe(onChange: () => void): () => void {
  listeners.add(onChange);
  return () => listeners.delete(onChange);
}

function readSnapshot(): LiveSnapshot | null {
  return snapshot;
}

function readServerSnapshot(): LiveSnapshot | null {
  return null;
}

export function writeHerdSignalsLiveSnapshot(key: string, data: HerdSignalsLiveResponse): void {
  snapshot = { key, data, receivedAt: Date.now() };
  for (const listener of listeners) listener();
}

export function useHerdSignalsLiveSnapshot(key: string): LiveSnapshot | null {
  const current = useSyncExternalStore(subscribe, readSnapshot, readServerSnapshot);
  return current?.key === key ? current : null;
}

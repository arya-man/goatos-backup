"use client";

import { createContext, useContext } from "react";
import type { Park } from "@/lib/scope";

/**
 * The parks the shell's scope switcher offers (MeshaShell `parks`). A route skeleton whose loaded
 * page draws one control per park (the Sales farm tabs) reads these, so the placeholder has the
 * same number of tabs as the page that replaces it.
 */
export const ShellParksContext = createContext<readonly Park[]>([]);

export function useShellParks(): readonly Park[] {
  return useContext(ShellParksContext);
}

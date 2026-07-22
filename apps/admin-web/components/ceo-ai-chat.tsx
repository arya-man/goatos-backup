"use client";

import type { ReactElement } from "react";
import { CeoAiPanel, type AssistantCopy } from "@/features/ceo-ai";

// Backward-compatible entrypoint for the leadership assistant, mounted by
// components/mesha-shell.tsx. The real product surface (streaming answers,
// conversation threads, citations, feedback, starters, honest states) lives in
// features/ceo-ai. Leadership visibility is decided SERVER-SIDE by the backend
// capability probe (GET /api/ceo-ai/starters -> backend ceo_internal gate), NOT
// by a client display-name regex. `displayName`/`subtitle` are retained only
// for prop compatibility with the shell and are no longer a security boundary.

export type CEOAIChatCopy = AssistantCopy;

export function CEOAIChat({
  displayName: _displayName,
  subtitle: _subtitle,
  copy,
}: {
  displayName: string;
  subtitle: string;
  copy: CEOAIChatCopy;
}): ReactElement {
  void _displayName;
  void _subtitle;
  return <CeoAiPanel copy={copy} />;
}

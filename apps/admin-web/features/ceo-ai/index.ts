// Public entrypoint for the leadership assistant feature. Other modules (e.g.
// components/ceo-ai-chat.tsx, mesha-shell) import the assistant through this
// barrel only — never a deep path into the feature internals — per the
// admin-web feature-boundary guard.
export { CeoAiPanel } from "./ceo-ai-panel";
export type { AssistantCopy } from "./types";

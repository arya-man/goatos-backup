// Public entrypoint for the generic Config (protocol rules) feature. App pages must import from
// "@/features/config" per the import-boundary guardrail. This surface is category-agnostic — it is NOT
// owned by any single vertical/module. See obligation-engine.md §2.1 (config-UI contract).
export { ConfigProtocolRulesPage } from "./protocol-rules-page";

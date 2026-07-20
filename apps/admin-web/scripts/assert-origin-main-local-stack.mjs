import path from "node:path";
import { fileURLToPath } from "node:url";
import { assertOriginMainLocalStack } from "./lib/origin-main-local-stack-guard.mjs";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
assertOriginMainLocalStack(repoRoot, "admin-web");

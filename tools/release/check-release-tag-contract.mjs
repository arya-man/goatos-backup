#!/usr/bin/env node
import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";

const failures = [];

function read(path) {
  return readFileSync(path, "utf8");
}

function mustInclude(path, needle, message) {
  const body = read(path);
  if (!body.includes(needle)) failures.push(`${path}: ${message}`);
}

mustInclude(
  "Makefile",
  "release-tag:",
  "must expose make release-tag so humans, Codex, and Claude use one release tagging path",
);
mustInclude(
  "tools/deploy/stg-clouddeploy-release.sh",
  "tools/release/create-release-tag.sh",
  "STG Cloud Deploy helper must create the GitHub release tag after verified rollout",
);
mustInclude(
  "docs/mobile/stg-signed-release.md",
  "FIREBASE_RELEASE_URL",
  "Firebase runbook must tell release builders to record the Firebase release URL in the GitHub tag",
);
mustInclude(
  "docs/runbooks/release-tags.md",
  "Backend",
  "release tag runbook must document Backend section",
);
mustInclude(
  "docs/runbooks/release-tags.md",
  "Frontend/Admin Web",
  "release tag runbook must document Frontend/Admin Web section",
);
mustInclude(
  "docs/runbooks/release-tags.md",
  "Mobile Android",
  "release tag runbook must document Mobile Android section",
);
mustInclude(
  "AGENTS.md",
  "make release-tag",
  "agent instructions must route release tagging through make release-tag",
);
mustInclude(
  "SKILLS.md",
  "make release-tag",
  "skill index must route release tagging through make release-tag",
);

try {
  const output = execFileSync(
    "bash",
    [
      "tools/release/create-release-tag.sh",
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        DRY_RUN: "1",
        PUSH_TAG: "0",
        ENV: "stg",
        CLOUD_DEPLOY_RELEASE: "contract-test",
        FIREBASE_RELEASE_URL: "https://console.firebase.google.com/project/goatos-stg/appdistribution/test",
        ANDROID_VERSION: "0.0.0-stg",
        ANDROID_VERSION_CODE: "1",
      },
    },
  );
  for (const section of [
    "Backend",
    "Frontend/Admin Web",
    "Mobile Android",
    "Infra/Deploy",
    "Docs/Seed/Data",
    "Other",
    "Firebase Android: 0.0.0-stg / versionCode 1",
    "Firebase release: https://console.firebase.google.com/project/goatos-stg/appdistribution/test",
    "Cloud Deploy: contract-test",
  ]) {
    if (!output.includes(section)) failures.push(`create-release-tag dry run omitted ${section}`);
  }
} catch (error) {
  failures.push(`create-release-tag dry run failed: ${error.message}`);
}

try {
  const deploy = read("tools/deploy/stg-clouddeploy-release.sh");
  const verifyIndex = deploy.indexOf("verify_stg_images");
  const tagIndex = deploy.indexOf("tools/release/create-release-tag.sh");
  if (verifyIndex < 0 || tagIndex < 0 || tagIndex < verifyIndex) {
    failures.push("STG deploy helper must invoke create-release-tag.sh after verify_stg_images");
  }
  if (deploy.includes("GOATOS_CREATE_RELEASE_TAG")) {
    failures.push("STG deploy helper must not expose a normal release-tag bypass");
  }
} catch (error) {
  failures.push(`could not inspect STG deploy helper: ${error.message}`);
}

if (failures.length) {
  console.error("release-tag-contract: FAIL");
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log("release-tag-contract: PASS");

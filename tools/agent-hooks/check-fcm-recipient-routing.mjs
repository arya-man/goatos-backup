#!/usr/bin/env node
// check-fcm-recipient-routing.mjs — blocks the role-visible-but-no-push anti-pattern.
//
// FCM sends to device tokens; Goat OS must first resolve which active devices
// belong to the role audience for an alert. Tenant leadership role alerts must
// use the same active user_scope_grants truth that grants app access, not only
// workforce_positions seats. Seed scripts must not fabricate notification
// routing as a side effect; routing belongs in runtime recipient resolvers.

import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = process.argv.find((arg) => arg.startsWith("--test-dir="))
  ? process.argv.find((arg) => arg.startsWith("--test-dir=")).split("=")[1]
  : resolve(import.meta.dirname, "../..");

function fail(message) {
  console.error(`FAIL: ${message}`);
  process.exit(1);
}

function pass(message) {
  console.log(`PASS: ${message}`);
}

function findings({ rosterRepository, stgSeed }) {
  const out = [];

  const single = /func \(r \*Repository\) ResolvePositionRecipients\([\s\S]*?func \(r \*Repository\) ResolvePositionRecipientsBatch/.exec(rosterRepository)?.[0] ?? "";
  const batch = /func \(r \*Repository\) ResolvePositionRecipientsBatch\([\s\S]*?func scanNotificationRecipients/.exec(rosterRepository)?.[0] ?? "";

  for (const [name, source] of [["ResolvePositionRecipients", single], ["ResolvePositionRecipientsBatch", batch]]) {
    if (!source.includes("user_scope_grants")) {
      out.push(`${name}: tenant role push recipients must include user_scope_grants, not only workforce_positions`);
    }
    for (const role of ["ceo_internal", "pc_director", "growth_director"]) {
      if (!source.includes(role)) {
        out.push(`${name}: missing tenant leadership role ${role}`);
      }
    }
    if (!source.includes("workforce_member_devices") || !source.includes("fcm_token IS NOT NULL")) {
      out.push(`${name}: must resolve active devices with non-null fcm_token`);
    }
  }

  for (const token of ["ensureNotificationRouting", "notify_routes", "position_module_duties", "proof.verify"]) {
    if (stgSeed.includes(token)) {
      out.push(`seed-stg-login-grants: must not seed notification routing token ${token}`);
    }
  }

  return out;
}

function readGoPackageSource(dir) {
  return readdirSync(dir)
    .filter((name) => name.endsWith(".go"))
    .sort()
    .map((name) => readFileSync(resolve(dir, name), "utf8"))
    .join("\n");
}

function selfTest() {
  const goodRoster = `
func (r *Repository) ResolvePositionRecipients() {
  SELECT * FROM user_scope_grants JOIN workforce_member_devices ON d.fcm_token IS NOT NULL
  WHERE g.role = ANY(ARRAY['ceo_internal','pc_director','growth_director'])
}
func (r *Repository) ResolvePositionRecipientsBatch() {
  SELECT * FROM user_scope_grants JOIN workforce_member_devices ON d.fcm_token IS NOT NULL
  WHERE g.role = ANY(ARRAY['ceo_internal','pc_director','growth_director'])
}
func scanNotificationRecipients() {}
`;
  const goodSeed = `login grants and mobile profile only`;
  if (findings({ rosterRepository: goodRoster, stgSeed: goodSeed }).length !== 0) {
    throw new Error("self-test: compliant sources produced findings");
  }

  const noGrants = findings({ rosterRepository: goodRoster.replaceAll("user_scope_grants", "workforce_positions"), stgSeed: goodSeed });
  if (!noGrants.some((x) => x.includes("user_scope_grants"))) {
    throw new Error(`self-test: missed grants-only routing regression: ${JSON.stringify(noGrants)}`);
  }

  const seedCoupling = findings({ rosterRepository: goodRoster, stgSeed: `ensureNotificationRouting proof.verify position_module_duties notify_routes` });
  if (!seedCoupling.some((x) => x.includes("must not seed notification routing"))) {
    throw new Error(`self-test: missed seed notification coupling regression: ${JSON.stringify(seedCoupling)}`);
  }
  pass("fcm-recipient-routing self-test");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const result = findings({
  rosterRepository: readFileSync(resolve(repo, "backend/internal/workforce/adapters/postgres/roster_repository.go"), "utf8"),
  stgSeed: readGoPackageSource(resolve(repo, "backend/cmd/seed-stg-login-grants")),
});

if (result.length > 0) {
  fail(result.join("\n"));
}
pass("FCM recipient routing uses app-role truth and seed-time notification routing is absent");

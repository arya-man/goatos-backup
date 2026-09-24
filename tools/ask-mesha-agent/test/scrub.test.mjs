import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { scrub, nameIsSecret, contentIsSecret } from "../deploy/scrub-snapshot.mjs";

// Fake secrets are assembled at runtime so secret scanners don't flag this test file.
const fake = (...parts) => parts.join("");

test("names: secrets removed, examples and code kept", () => {
  for (const n of [".env", ".env.prod", "id_ed25519", "prod-sa.json", "service_account_x.json", "db-credentials.yaml", "client_secret.json", "x.pem", ".pgenv", ".npmrc"]) assert.ok(nameIsSecret(n), n);
  for (const n of [".env.example", "secrets.go.example", "server.mjs", "README.md", "credentials_test.go.sample", "secretary.ts", "secrets.tf", "fetch-ceo-ai-secrets.sh", "stg-operator-login-credentials.md", "id_ed25519.pub"]) assert.ok(!nameIsSecret(n), n);
});

test("content: real-looking secrets caught, placeholders not", () => {
  assert.ok(contentIsSecret("key: -----BEGIN PRIVATE KEY-----\nabc"));
  assert.ok(contentIsSecret('{"private_key": "-----BEGIN PRIVATE KEY-----"}'));
  assert.ok(contentIsSecret(fake("ANTHROPIC_API_KEY=", "sk-ant-", "api03-", "x".repeat(40))));
  assert.ok(contentIsSecret(fake("AK", "IA", "Q".repeat(16))));
  assert.ok(contentIsSecret(fake("url=postgres://", "u", ":", "Zz", "9".repeat(8), "@", "10.0.0.5:5432/db")));
  assert.ok(!contentIsSecret("url=postgres://user:password@localhost:5432/db"));
  assert.ok(!contentIsSecret("postgres://$PGUSER:$PGPASSWORD@host/db"));
  assert.ok(!contentIsSecret("const x = 'sk-ant-...'; // placeholder"));
});

test("scrub deletes by name and content, keeps the rest", () => {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), "scrub-"));
  fs.mkdirSync(path.join(d, "a/b"), { recursive: true });
  fs.writeFileSync(path.join(d, "a/.env"), "X=1");
  fs.writeFileSync(path.join(d, "a/b/notes.txt"), fake("token gh", "p_", "A".repeat(36)));
  fs.writeFileSync(path.join(d, "a/b/main.go"), "package main");
  fs.symlinkSync(path.join(d, "a/b"), path.join(d, "inside"));
  fs.symlinkSync(os.tmpdir(), path.join(d, "outside"));
  const removed = scrub(d).map(([p, why]) => [path.relative(d, p), why]);
  assert.deepEqual(removed.sort(), [["a/.env", "name"], ["a/b/notes.txt", "content"], ["outside", "symlink outside snapshot"]]);
  assert.ok(fs.existsSync(path.join(d, "a/b/main.go")));
});

test("newer token kinds caught; placeholders and loopback kept", () => {
  assert.ok(contentIsSecret(fake("token=github", "_pat_", "A".repeat(60))));
  assert.ok(!contentIsSecret("postgres://u:<password>@host/db"));
  assert.ok(!contentIsSecret("postgres://goatos:devpass123@[::1]:5432/goatos"));
});

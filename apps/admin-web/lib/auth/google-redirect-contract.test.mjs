import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const here = new URL(".", import.meta.url).pathname;
const loginSource = readFileSync(join(here, "../../components/auth/google-login.tsx"), "utf8");
const routeSource = readFileSync(join(here, "../../app/api/auth/google-redirect/route.ts"), "utf8");

test("Google SSO uses the callback popup flow without a redirect URI dependency", () => {
  assert.match(loginSource, /ux_mode:\s*"popup"/);
  assert.doesNotMatch(loginSource, /login_uri:\s*loginUri/);
  assert.doesNotMatch(loginSource, /use_fedcm_for_button:\s*true/);
  assert.doesNotMatch(loginSource, /use_fedcm_for_prompt:\s*true/);
});

test("Google redirect callback validates CSRF and stores only a short-lived credential", () => {
  assert.match(routeSource, /csrfBody\s*\|\|\s*csrfBody\s*!==\s*csrfCookie/);
  assert.match(routeSource, /GOOGLE_REDIRECT_CREDENTIAL_MAX_AGE_SECONDS/);
  assert.match(routeSource, /httpOnly:\s*true/);
  assert.match(routeSource, /maxAge:\s*0/);
});

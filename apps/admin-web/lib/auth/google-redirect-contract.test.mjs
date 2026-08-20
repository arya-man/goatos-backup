import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const here = new URL(".", import.meta.url).pathname;
const loginSource = readFileSync(join(here, "../../components/auth/google-login.tsx"), "utf8");
const routeSource = readFileSync(join(here, "../../app/api/auth/google-redirect/route.ts"), "utf8");

test("Google SSO uses redirect mode instead of the transform-popup button flow", () => {
  assert.match(loginSource, /ux_mode:\s*"redirect"/);
  assert.match(loginSource, /login_uri:\s*loginUri/);
  assert.doesNotMatch(loginSource, /ux_mode:\s*"popup"/);
  assert.doesNotMatch(loginSource, /use_fedcm_for_button:\s*true/);
  assert.doesNotMatch(loginSource, /use_fedcm_for_prompt:\s*true/);
});

test("Google redirect callback validates CSRF and stores only a short-lived credential", () => {
  assert.match(routeSource, /csrfBody\s*\|\|\s*csrfBody\s*!==\s*csrfCookie/);
  assert.match(routeSource, /GOOGLE_REDIRECT_CREDENTIAL_MAX_AGE_SECONDS/);
  assert.match(routeSource, /httpOnly:\s*true/);
  assert.match(routeSource, /maxAge:\s*0/);
});

test("Google redirect callback returns to the public dashboard origin behind Cloud Run", () => {
  assert.match(routeSource, /publicRequestOrigin\(request\)/);
  assert.match(routeSource, /GOATOS_CANONICAL_DASHBOARD_HOST/);
  assert.match(routeSource, /x-forwarded-host/);
  assert.doesNotMatch(routeSource, /new URL\(LOGIN_PATH,\s*request\.url\)/);
});

// The hop back from Google re-renders /login with the credential still to be exchanged. Without a
// completing state that render reads as "you are back at the sign-in page" for the seconds Firebase
// and the session exchange take, which is what operators report as being bounced to login.
const loginPageSource = readFileSync(join(here, "../../app/login/page.tsx"), "utf8");

test("the return hop from Google renders as a sign-in in progress, not a fresh sign-in form", () => {
  assert.match(loginPageSource, /google_redirect\S*\)\s*===\s*"1"/);
  assert.match(loginPageSource, /completingGoogleRedirect=\{completingGoogleRedirect\}/);
  assert.match(loginSource, /completingGoogleRedirect \? "signing_in" : "loading"/);
  // The sign-in controls stand down only while completing; a failed exchange must bring them back.
  assert.match(loginSource, /const showSignInControls = !completingRedirect;/);
  assert.match(loginSource, /setCompletingRedirect\(false\)/);
});

// Per-key copy fallbacks for the top-bar push-permission control.
//
// The SOURCE OF TRUTH is the backend bootstrap chrome copy map
// (chromeCopy() in backend/internal/adminui/app/service.go), which the shell passes down as
// `contractCopy`. This map is only a deploy-skew net, the same shape and the same rationale as
// COPY_FALLBACKS in ./admin-ui-contract.ts: the control renders inside the top bar on EVERY admin
// route, so a frontend deployed one release ahead of the backend -- or any older backend that does
// not serve a key yet -- must still render the control rather than a blank string or a throw.
// shellCopy() in mesha-shell.tsx throws on a missing key, which is correct for a key that has
// always existed and wrong here.
//
// It lives in lib/ rather than beside the component deliberately: check-ui-contract-literals.mjs
// scans app/, components/ and features/ for visible literals, and a per-key fallback map cannot
// satisfy a rule that forbids literals. Keeping it next to the repo's other copy-fallback map is
// the established shape, not a new exception. Delete a key here only once no served backend can
// omit it.
const PUSH_COPY_FALLBACKS: Record<string, string> = {
  "push.checking": "Notifications",
  "push.checking_label": "Checking notification support",
  "push.enable": "Enable notifications",
  "push.enable_hint": "Get notified in this browser",
  "push.enabling": "Turning on",
  "push.enabled": "Notifications on",
  "push.disable_hint": "Turn off notifications in this browser",
  "push.disabling": "Turning off",
  "push.this_browser_only": "This browser only. Each browser and profile is enabled separately.",
  "push.dismissed":
    "No choice was made. Click again when you are ready.",
  "push.blocked":
    "Notifications are blocked for this site. Turn them back on in your browser's site settings (the icon beside the address bar), then reload this page.",
  // The retryable timeout. It must NOT read like a refusal or like a misconfiguration: permission
  // was granted and nothing is known to be wrong, so it says what happened and invites the retry
  // that the Enable button beside it offers.
  "push.timed_out":
    "That took too long and did not finish. Nothing is switched on yet — click again to retry.",
  "push.retry": "Try again",
  // Nothing the reader can do fixes either of these, so each says that plainly and names who
  // can. They replace the raw engineering `state.reason` the control used to print verbatim.
  "push.unconfigured":
    "Notifications are not set up for this site yet. Ask your Goat OS administrator to switch them on.",
  "push.unsupported":
    "This browser cannot show notifications here. Open the dashboard in Chrome on a laptop or an Android phone, on its secure (https) address, to get them.",
};

export function pushCopy(contractCopy: Record<string, string> | undefined, key: string): string {
  const fromContract = contractCopy?.[key];
  if (typeof fromContract === "string" && fromContract.trim() !== "") return fromContract;
  return PUSH_COPY_FALLBACKS[key] ?? "";
}

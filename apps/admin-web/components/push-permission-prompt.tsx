"use client";

import { useCallback, useEffect, useState } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import CircularProgress from "@mui/material/CircularProgress";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import {
  detectWebPushSupport,
  disableWebPush,
  enableWebPush,
  readNotificationPermission,
  refreshWebPushRegistration,
  resolveWebPushState,
  type WebPushState,
} from "@/lib/web-push";
import { pushCopy } from "@/lib/push-copy";
import { raceControl } from "@/lib/control-race";

/**
 * The "Enable notifications" control for browser push.
 *
 * IT IS BEHIND A CLICK, ALWAYS. Nothing in this component asks for notification permission on
 * mount — Chrome treats a load-time permission request as abusive (auto-denies it and applies the
 * quieter UI to the origin for good), and interrupting someone with a browser modal they did not
 * ask for is a dark pattern regardless of what Chrome does about it.
 *
 * What DOES happen on mount is the silent half: if this browser already granted permission, the
 * FCM token is re-read and re-registered, because Chrome rotates that token and invalidates the
 * subscription on profile clear or a long idle stretch and tells the server nothing. A person who
 * turned notifications on last month must not silently stop receiving them.
 *
 * MOUNTING: this is a self-contained control that owns all its own state, so it sits wherever the
 * shell's notification affordance lives. Today that is the top-bar bell's panel: it is passed as
 * `NotificationBell`'s `permissionSlot` in components/mesha-shell.tsx, so the permission ask lands
 * where a person already goes to read their notifications instead of becoming a second control in
 * the chrome. That means it renders on EVERY admin route — see the copy note below for why nothing
 * in here may throw. On its own it renders as a labelled button and is usable as-is.
 */

/**
 * Visible copy. The real owner of these strings is the backend bootstrap contract: every key
 * below is now served in `AdminWebBootstrapResponse.copy` (backend/internal/adminui/app/service.go
 * `chromeCopy()`, alongside the existing top-bar/chrome keys) and is config-overridable through
 * the generic `chrome.copy.<key>` global entry. The shell passes that map in as `contractCopy`.
 *
 * The map below is a per-key fallback, NOT the source of truth -- the same shape as
 * lib/admin-ui-contract.ts's COPY_FALLBACKS and the notification centre's
 * NOTIFICATION_COPY_FALLBACKS. It exists because this control renders inside the top bar on
 * EVERY admin route: a frontend deployed one release ahead of the backend (or any older backend
 * that does not serve a key yet) must render the control, not a blank string and not a throw.
 * `shellCopy()` in mesha-shell.tsx throws on a missing key, which is right for a key that has
 * always existed and wrong here. Delete a fallback key only once no served backend can omit it.
 */


type Busy = "idle" | "enabling" | "disabling" | "refreshing";
const CONTROL_ACTION_TIMEOUT_MS = 30_000;
// A pending press stays readable in the brand colour (never the faded disabled look).
const PENDING_SX = { "&.Mui-disabled": { color: "primary.main", borderColor: "primary.main" } } as const;

const enableTimedOutState = (): WebPushState => ({ status: "timed_out" });
export function PushPermissionPrompt({
  className,
  contractCopy,
}: {
  className?: string;
  /** The bootstrap contract's copy map. Backend keys win over the local fallbacks. */
  contractCopy?: Record<string, string>;
}) {
  const copy = (key: string) => pushCopy(contractCopy, key);
  // Start from a state that is TRUE ON THE SERVER: no browser APIs exist during SSR, so claiming
  // "prompt" would flash an actionable button at a browser that cannot do it. `refreshing` renders
  // as a quiet, disabled control and is replaced on the first client effect.
  const [state, setState] = useState<WebPushState>({ status: "unsupported", reason: "" });
  const [busy, setBusy] = useState<Busy>("refreshing");
  const [message, setMessage] = useState("");

  useEffect(() => {
    let cancelled = false;
    // One async pass, so no setState happens synchronously inside the effect body (which triggers
    // a cascading render). The first await point is the honest pre-network paint.
    void (async () => {
      const support = detectWebPushSupport();
      const permission = readNotificationPermission();

      // Paint the honest pre-network state (supported? blocked? never asked?) so the control is
      // never blank while the silent refresh runs.
      const initial = resolveWebPushState({
        supported: support.supported,
        supportReason: support.reason,
        // Configuration is a server fact and is not known yet; assume configured so a supported
        // browser reads "prompt" rather than flashing "not configured" and then correcting itself.
        // The optimism is now nearly always RIGHT rather than merely kind: an absent VAPID key
        // means "use the Firebase SDK's own default key" and is configured enough to deliver, so
        // the only way the refresh below corrects this to `unconfigured` is a key that IS set and
        // is unusable -- rare, and a deployment mistake rather than a normal state.
        configured: true,
        permission,
        hasRegistration: false,
        browserInstallId: "",
      });
      if (cancelled) return;
      setState(initial);

      if (!support.supported || permission !== "granted") {
        setBusy("idle");
        return;
      }

      const next = await refreshWebPushRegistration();
      if (cancelled) return;
      setState(next);
      setBusy("idle");
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  const onEnable = useCallback(() => {
    setBusy("enabling");
    setMessage("");
    void raceControl<WebPushState>(enableWebPush(), enableTimedOutState, CONTROL_ACTION_TIMEOUT_MS)
      .then((next) => {
        setState(next);
        if (next.status === "dismissed") {
          setMessage(pushCopy(contractCopy, "push.dismissed"));
        }
      })
      .finally(() => setBusy("idle"));
  }, [contractCopy]);

  const onDisable = useCallback(() => {
    setBusy("disabling");
    setMessage("");
    void raceControl<WebPushState>(
      disableWebPush(),
      (): WebPushState => ({
        status: "error",
        reason: pushCopy(contractCopy, "push.disable_timed_out"),
      }),
      CONTROL_ACTION_TIMEOUT_MS,
    )
      .then((next) => setState(next))
      .finally(() => setBusy("idle"));
  }, [contractCopy]);

  const pending = busy !== "idle";

  if (state.status === "unsupported" && busy === "refreshing") {
    return (
      <Box className={className} aria-live="polite">
        <Button size="small" variant="outlined" color="inherit" disabled aria-label={copy("push.checking_label")} startIcon={<CircularProgress size={16} color="inherit" />}>
          {copy("push.checking")}
        </Button>
      </Box>
    );
  }

  const note = (text: React.ReactNode, caption = false) => (
    <Typography variant={caption ? "caption" : "body2"} component="p" sx={{ mt: 0.5, color: caption ? "text.secondary" : "text.primary" }}>
      {text}
    </Typography>
  );

  return (
    <Box className={className} aria-live="polite">
      {state.status === "enabled" ? (
        <Button
          size="small"
          variant="outlined"
          color="inherit"
          disabled={pending}
          onClick={onDisable}
          title={copy("push.disable_hint")}
          aria-busy={busy === "disabling"}
          sx={busy === "disabling" ? PENDING_SX : undefined}
          startIcon={busy === "disabling" ? <CircularProgress size={16} color="inherit" /> : <Iconify icon="solar:bell-bing-bold-duotone" width={16} />}
        >
          {copy(busy === "disabling" ? "push.disabling" : "push.enabled")}
        </Button>
      ) : null}

      {state.status === "prompt" ||
      state.status === "dismissed" ||
      state.status === "timed_out" ||
      state.status === "error" ? (
        <Button
          size="small"
          variant="outlined"
          color="inherit"
          disabled={pending}
          onClick={onEnable}
          title={copy("push.enable_hint")}
          aria-busy={busy === "enabling"}
          sx={busy === "enabling" ? PENDING_SX : undefined}
          startIcon={busy === "enabling" ? <CircularProgress size={16} color="inherit" /> : <Iconify icon="solar:bell-bing-bold" width={16} />}
        >
          {copy(
            busy === "enabling"
              ? "push.enabling"
              : state.status === "timed_out"
                ? "push.retry"
                : "push.enable",
          )}
        </Button>
      ) : null}

      {state.status === "blocked" ||
      state.status === "unsupported" ||
      state.status === "unconfigured" ? (
        <Stack direction="row" spacing={1} sx={{ alignItems: "center" }}>
          <Iconify icon="solar:bell-off-bold" width={16} sx={{ flexShrink: 0 }} />
          <Typography variant="body2" component="span">
            {copy(
              state.status === "blocked"
                ? "push.blocked"
                : state.status === "unconfigured"
                  ? "push.unconfigured"
                  : "push.unsupported",
            )}
          </Typography>
        </Stack>
      ) : null}

      {state.status === "timed_out" ? note(copy("push.timed_out")) : null}
      {state.status === "error" ? note(state.reason) : null}
      {message ? note(message) : null}
      {state.status === "enabled" && !pending ? note(copy("push.this_browser_only"), true) : null}
    </Box>
  );
}

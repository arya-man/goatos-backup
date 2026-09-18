"use client";

/**
 * The web-push client, kept OUT of the shared shell bundle.
 *
 * WHY THIS FILE EXISTS. `PushPermissionPrompt` pulls in `@/lib/web-push`, which pulls in
 * `firebase/app` + `firebase/messaging`. The shell mounts it on EVERY admin route (it is the
 * notification bell's `permissionSlot`), so every one of the app's 63 routes was paying for the
 * push client in its globally shared JS -- measured at +15.3 KB gzip app-wide against origin/main,
 * for a control that only exists inside a panel most readers never open.
 *
 * TWO HALVES, because the component does two unrelated things and only one of them is deferrable:
 *
 *   1. THE CONTROL (the "Enable notifications" button, its copy, its state machine) is behind a
 *      click by definition -- it lives inside the bell's panel -- so it is `lazy()`d here. Creating
 *      the element is free; React only fetches the chunk when the panel actually renders it, which
 *      is the first time someone opens the bell.
 *
 *   2. THE SILENT RE-REGISTRATION is NOT deferrable and must not be lost. Chrome rotates the FCM
 *      token and invalidates the subscription on profile clear or a long idle stretch, and tells
 *      the server nothing; a person who turned notifications on last month must not silently stop
 *      receiving them. That refresh used to ride this component's mount effect -- which now only
 *      runs when the panel opens. `PushRegistrationSync` below keeps it on mount, and it is CHEAP
 *      because it reads `Notification.permission` (a browser global, no import) and only then
 *      dynamically imports the push client. A browser that never granted permission -- the
 *      overwhelming majority, and every SSR pass -- loads no firebase at all.
 *
 * `next/dynamic` is deliberately not used: the repo has no precedent for it, and the two dynamic
 * imports it already has (`features/people/people-page.tsx`, `lib/firebase-performance.ts`) use a
 * plain `await import()`. `React.lazy` is that same primitive with a render boundary, so this file
 * follows the existing grain rather than introducing a second mechanism.
 */

import { Suspense, lazy, useEffect } from "react";

const PushPermissionPrompt = lazy(async () => {
  const mod = await import("./push-permission-prompt");
  return { default: mod.PushPermissionPrompt };
});

/**
 * The deferred control. `fallback={null}` rather than a spinner: the component's OWN first paint is
 * already a quiet disabled "Notifications" button while it probes the browser, so a second
 * placeholder would only add a flicker inside a panel that has just animated in.
 */
export function PushPermissionPromptLazy({
  className,
  contractCopy,
}: {
  className?: string;
  contractCopy?: Record<string, string>;
}) {
  return (
    <Suspense fallback={null}>
      <PushPermissionPrompt className={className} contractCopy={contractCopy} />
    </Suspense>
  );
}

/**
 * Mount-time token refresh for a browser that ALREADY granted permission. Renders nothing.
 *
 * It never asks for permission and never touches the UI -- Chrome treats a load-time permission
 * request as abusive, and that rule is unchanged: only a browser already at `granted` reaches the
 * import. Every failure is swallowed for the same reason the bell swallows its own: a rotated
 * token, a missing service worker or a 503 from the config endpoint must leave every screen in the
 * app rendering exactly as before.
 */
export function PushRegistrationSync() {
  useEffect(() => {
    let cancelled = false;
    // Read the permission from the browser global. No import, no work, and it is the whole gate:
    // anything other than "granted" has nothing to re-register.
    if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
    void (async () => {
      try {
        const { refreshWebPushRegistration } = await import("@/lib/web-push");
        if (cancelled) return;
        await refreshWebPushRegistration();
      } catch {
        // Silent on purpose: see the note above.
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);
  return null;
}

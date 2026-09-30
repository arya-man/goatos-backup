"use client";

import { useEffect } from "react";

import { replaceLocalOverlayUrl } from "@/components/local-overlay-link";
import Alert from "@mui/material/Alert";
import Link from "@mui/material/Link";

/**
 * The recovery half of a dead ?hc_version= / ?hc_register=.
 *
 * A selected version that no longer resolves is a REAL state, not an edge case: the author
 * publishes a draft (which RETIRES the id the editor URL is holding), or a second tab discards
 * it. The server already recovers by rendering the LIST underneath this notice, so the reader is
 * never left on a blank screen with one link to press.
 *
 * What this adds is the URL. The dead id is still in the address bar after that server render, so
 * a reload -- or a Back into this entry -- would ask for the same missing version again and the
 * notice would follow the author around until they happened to click away. Replacing the URL with
 * the list href retires the id once the page is on screen.
 *
 * It REPLACES rather than pushes: the dead editor state is not somewhere a reader chose to be, so
 * it must not become a history entry of its own. Back therefore returns to wherever they were
 * before opening the editor, not to the error they just recovered from.
 */
export function StaleVersionRecovery({ listHref }: { listHref: string }) {
  useEffect(() => {
    try {
      const here = new URL(window.location.href);
      const target = new URL(listHref, window.location.href);
      // Only ever narrows this page's own query. A path change here would be a navigation, which
      // is the server's job, never a side effect of rendering a notice.
      if (here.pathname !== target.pathname) return;
      if (here.search === target.search) return;
      replaceLocalOverlayUrl(`${target.pathname}${target.search}`);
    } catch {
      // A malformed href is not worth breaking the recovered page over: the notice and the list
      // are already rendered, and the reader can still press the link.
    }
  }, [listHref]);

  return null;
}

/** The notice itself, rendered ABOVE the list the server recovered to. */
export function StaleVersionNotice({
  message,
  linkLabel,
  listHref,
}: {
  message: string;
  linkLabel: string;
  listHref: string;
}) {
  return (
    <Alert severity="error" sx={{ mb: 2 }}><div>
        {message}{" "}
        <Link href={listHref} color="inherit" underline="always" sx={{ whiteSpace: "nowrap" }}>
          {linkLabel}
        </Link>
      </div>
      <StaleVersionRecovery listHref={listHref} />
    </Alert>
  );
}

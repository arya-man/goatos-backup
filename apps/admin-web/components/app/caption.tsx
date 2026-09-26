import type { ReactNode } from "react";

import Typography from "@mui/material/Typography";

import { InfoHint } from "@/components/app/info-hint";

/** Anything longer than a short caption is explanation, and explanation lives behind the info glyph. */
export const CAPTION_MAX = 72;

/**
 * A card/section caption: a short string renders as one muted line; a sentence longer than
 * `CAPTION_MAX` renders as an info glyph with the sentence as its tooltip (no paragraph on the
 * page, nothing hidden by CSS). Non-string children render as given.
 *
 * Server-safe. Renders MUI Typography for the short case; the long-caption fallback delegates
 * to `<InfoHint>` directly (no wrapping span, no className hooks — theme props only).
 */
export function Caption({ children, className }: { children: ReactNode; className?: string }) {
  if (typeof children === "string" && children.trim().length > CAPTION_MAX) {
    return <InfoHint text={children.trim()} className={className} />;
  }
  return (
    <Typography
      component="p"
      variant="body2"
      color="text.secondary"
      className={className}
      sx={{ mt: 0.25 }}
    >
      {children}
    </Typography>
  );
}

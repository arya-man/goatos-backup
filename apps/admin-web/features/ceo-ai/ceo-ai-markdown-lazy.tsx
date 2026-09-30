"use client";

import { lazy, Suspense, type ReactElement } from "react";
import Box from "@mui/material/Box";
import { markdownFallbackSx } from "./ceo-ai-sx";

// react-markdown + remark-gfm + rehype-highlight (highlight.js) are only needed once the
// assistant panel shows an answer. Keeping them behind a lazy boundary removes ~100 KB gzip
// from the first-load JS of every admin page (the panel is mounted by the shell everywhere).
const loadMarkdown = () => import("./ceo-ai-markdown");
const LazyMarkdown = lazy(() => loadMarkdown().then((m) => ({ default: m.CeoAiMarkdown })));

/** Warm the markdown chunk (e.g. when the panel opens) so answers render formatted at once. */
export function preloadCeoAiMarkdown(): void {
  void loadMarkdown();
}

export function CeoAiMarkdown({ text }: { text: string }): ReactElement {
  return (
    <Suspense fallback={<Box sx={markdownFallbackSx}><p>{text}</p></Box>}>
      <LazyMarkdown text={text} />
    </Suspense>
  );
}

import { SourceEntrySkeleton } from "@/features/procurement/source-entry-skeletons";

/** /procurement/source-entry: the page's own twin (its rows are the UrlSuspense fallback too). */
export default function Loading() {
  return <SourceEntrySkeleton />;
}

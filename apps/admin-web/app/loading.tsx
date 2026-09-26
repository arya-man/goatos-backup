import { ShellSkeleton } from "@/components/shell-skeleton";

/**
 * Root-segment loader: the shell-shaped skeleton (top bar, sidebar column, page skeleton), so a
 * hard navigation never paints a blank page or two grey bars before the admin layout streams in.
 * Presentation only; the shimmer pauses under `prefers-reduced-motion` via `.kit-skeleton`.
 */
export default function Loading() {
  return <ShellSkeleton />;
}

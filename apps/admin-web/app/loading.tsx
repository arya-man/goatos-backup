import { ShellSkeleton } from "@/components/shell-skeleton";
import { EnsureAppTheme } from "@/theme/ensure-app-theme";

/**
 * Root-segment loader: the shell-shaped skeleton (top bar, sidebar column, page skeleton), so a
 * hard navigation never paints a blank page or two grey bars before the admin layout streams in.
 * Presentation only (MUI Skeleton blocks).
 *
 * Next also mounts this file in the prerendered `/_global-error` tree, which has NO root layout and
 * so no theme provider; EnsureAppTheme supplies the app theme there (and is a pass-through under
 * app/layout.tsx). guard: global-error-prerender-no-providers
 */
export default function Loading() {
  return (
    <EnsureAppTheme>
      <ShellSkeleton />
    </EnsureAppTheme>
  );
}

import { PageHeaderSkeleton } from "@/components/app/page-header";
import { Skeleton, SkeletonKpiRow, SkeletonTable } from "@/components/app/page-skeletons";
import "@/layouts/mesha-layout.css";
import "@/layouts/shell-skeleton.css";

/**
 * The shell, drawn before the bootstrap contract arrives. Same geometry as the MUI Minimal
 * DashboardLayout the shell renders (layouts/dashboard): a 300px nav column (88px when the reader
 * keeps the mini rail, from `html[data-nav-rail]`, set server-side from the rail cookie), a
 * 64/72px header, and the page column as `.main.msh-content > .wrap.msh-wrap`, so the first paint
 * does not jump when the real shell hydrates. The nav is hidden below 1200px, like the template.
 * No copy of its own beyond the wordmark tile.
 */
export function ShellSkeleton({ children }: { children?: React.ReactNode }) {
  return (
    <div className="shell-skeleton msh-skel" aria-busy="true">
      <aside className="msh-skel-nav" aria-hidden="true">
        <div className="msh-skel-logo">
          <span className="msh-skel-mark">मे</span>
        </div>
        {[6, 8].map((rows, group) => (
          <div key={group} className="msh-skel-group">
            <div className="msh-skel-subheader">
              <Skeleton width={group ? 56 : 64} height={10} />
            </div>
            {Array.from({ length: rows }, (_, i) => (
              <div key={i} className="msh-skel-item">
                <Skeleton width={24} height={24} radius={6} />
                <span className="msh-skel-label">
                  <Skeleton width={(group ? 80 : 90) + (i % (group ? 4 : 3)) * (group ? 16 : 18)} height={14} />
                </span>
              </div>
            ))}
          </div>
        ))}
      </aside>
      <div className="msh-skel-col">
        <div className="msh-skel-header" aria-hidden="true">
          <Skeleton width={40} height={40} radius={20} />
          <Skeleton width={40} height={40} radius={20} />
          <Skeleton width={40} height={40} radius={20} />
        </div>
        <main className="main msh-content msh-skel-main">
          <div className="wrap msh-wrap">{children ?? <GenericPageSkeleton />}</div>
        </main>
      </div>
    </div>
  );
}

/** The page-column skeleton for a route that has no loading.tsx of its own. */
export function GenericPageSkeleton() {
  return (
    <div className="kit-page">
      <PageHeaderSkeleton action />
      <SkeletonKpiRow count={4} />
      <SkeletonTable rows={8} widths={["1.6fr", "1fr", "1fr", "1fr", "88px"]} />
    </div>
  );
}

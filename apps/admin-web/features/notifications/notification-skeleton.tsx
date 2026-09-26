"use client";

/**
 * The eager Suspense fallback for the bell's lazy panel: header title + five skeleton rows, the
 * exact shape the panel resolves into. Kept tiny and separate from `./notification-panel` on
 * purpose -- it ships in the shell chunk of every route, so it imports only the kit Skeleton and
 * the panel's stylesheet (which the panel would load anyway).
 */

import { Skeleton } from "@/components/app/page-skeletons";
import "./notification-panel.css";

export function NotificationSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="nc" data-notification-panel-fallback aria-busy="true">
      <div className="nc-head">
        <Skeleton width={120} height={16} />
      </div>
      <div className="nc-scroll">
        <ul className="nc-items" aria-hidden="true">
          {Array.from({ length: rows }, (_, i) => (
            <li key={i} className="nc-row nc-row-sk">
              <Skeleton width={40} height={40} radius="50%" />
              <span className="nc-main">
                <Skeleton height={13} width={`${58 + ((i * 17) % 30)}%`} />
                <Skeleton height={10} width={`${28 + ((i * 23) % 22)}%`} />
              </span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

import type { ReactNode } from "react";

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";

/** The HRMS page header for the discipline pages: crumb, title and subtitle from the contract. */
export function HrmsFrame({ pageContract, children, aside }: { pageContract: AdminUiPageContract; children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="crumb">
            <b>{copy(pageContract, "crumb")}</b> · {pageContract.title}
          </div>
          <h1>{pageContract.title}</h1>
          <div className="sub">{pageContract.subtitle}</div>
        </div>
        {aside ? (
          <>
            <div className="sp" style={{ flex: 1 }} />
            {aside}
          </>
        ) : null}
      </div>
      {children}
    </div>
  );
}

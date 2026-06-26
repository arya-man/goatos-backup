// Shared CEO/admin role-preview lens model.
//
// ONE source of truth for the role lenses that the top-bar preview (MeshaShell) and the Audit Log
// `Viewing as` control both render. Per context/frontend/current-admin-web-scope.md, the Audit Log
// `Viewing as` must mirror these lenses instead of maintaining a separate hard-coded role list.
//
// This is PREVIEW state only. It lets a superadmin/CEO/COO see how role-scoped navigation, permissions,
// and Audit Log span would LOOK for another role. It NEVER grants authority and must never bypass backend
// RBAC — real permissions are server-enforced.

export type RoleLens = {
  id: string;
  // Full display name for the top-bar role menu.
  name: string;
  // Compact label for the Audit Log `Viewing as` chip row (mirrors the mock's auditShort).
  auditShort: string;
  // Scope summary (vertical/park reach) shown beside the name.
  scope: string;
  // One-line description for the role menu.
  description: string;
  superadmin?: boolean;
};

// Canonical visible lenses for this slice. Keep this list aligned to the active PHC Vaccination surface:
// leadership, PHC governance, park execution, ground execution, and read-only summary. Procurement/source
// entry remains a supporting screen, but it is not exposed as a separate live leadership lens here.
export const ROLE_LENSES: RoleLens[] = [
  {
    id: "coo",
    name: "Superadmin / CEO / COO",
    auditShort: "COO",
    scope: "all · deep",
    description: "Central Command · all parks",
    superadmin: true,
  },
  {
    id: "health-director",
    name: "Health Director",
    auditShort: "Health Dir",
    scope: "health vertical · all parks",
    description: "PHC / health governance view",
  },
  {
    id: "park-head",
    name: "Park Head · CBE",
    auditShort: "Park Head · CBE",
    scope: "all verticals · 1 park",
    description: "CBE park leadership view",
  },
  {
    id: "health-manager",
    name: "Health Mgr · CBE",
    auditShort: "Health Mgr · CBE",
    scope: "health vertical · 1 park",
    description: "CBE PHC manager view",
  },
  {
    id: "ground",
    name: "Assist / Ground · CBE",
    auditShort: "Assist · CBE",
    scope: "tasks · 1 park",
    description: "field execution queue",
  },
  {
    id: "investor",
    name: "Investor",
    auditShort: "Investor",
    scope: "read-only summary",
    description: "summary-only lens",
  },
];

export const DEFAULT_ROLE_LENS: RoleLens = ROLE_LENSES[0];

// Resolve a lens id (e.g. an Audit Log `viewing_as` URL param) to a lens; falls back to the default
// superadmin lens for unknown/missing ids so the preview can never land on an undefined role.
export function roleLensById(id?: string | null): RoleLens {
  if (!id) return DEFAULT_ROLE_LENS;
  return ROLE_LENSES.find((lens) => lens.id === id) ?? DEFAULT_ROLE_LENS;
}

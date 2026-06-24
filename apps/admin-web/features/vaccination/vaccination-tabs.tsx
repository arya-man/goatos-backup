import Link from "next/link";

export type VaccinationTab = "action" | "adherence" | "verify";

// PHC -> Vaccination sub-nav. Protocol Rules is generic Admin / Data Ops, not a Vaccination-owned
// tab; Vaccination pages link to /config?category=vaccination when contextual authoring is needed.
const tabs: Array<{ id: VaccinationTab; label: string; href: string }> = [
  { id: "action", label: "Action Center", href: "/vaccination" },
  { id: "adherence", label: "Adherence", href: "/vaccination/adherence" },
  { id: "verify", label: "Verification", href: "/vaccination?bucket=verify" },
];

// In-page navigation for the Vaccination module: PHC -> Vaccination -> these screens.
export function VaccinationTabs({ current }: { current: VaccinationTab }) {
  return (
    <div className="subtabs" style={{ marginBottom: 16 }}>
      {tabs.map((t) => (
        <Link key={t.id} href={t.href} className={t.id === current ? "on" : ""}>
          {t.label}
        </Link>
      ))}
    </div>
  );
}

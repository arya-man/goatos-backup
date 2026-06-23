import Link from "next/link";

export type VaccinationTab = "action" | "config" | "adherence" | "verify";

const tabs: Array<{ id: VaccinationTab; label: string; href: string }> = [
  { id: "action", label: "Action Center", href: "/vaccination" },
  { id: "config", label: "Config", href: "/vaccination/config" },
  { id: "adherence", label: "Adherence", href: "/vaccination/adherence" },
  { id: "verify", label: "Verification", href: "/vaccination?bucket=verify" },
];

// In-page navigation for the Vaccination vertical: PHC → Vaccination → these screens.
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

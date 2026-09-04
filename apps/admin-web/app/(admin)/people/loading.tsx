import { Users } from "lucide-react";

export default function Loading() {
  return (
    <div className="screen on">
      <div className="phead" style={{ marginTop: 12, alignItems: "flex-end", paddingBottom: 6 }}>
        <div>
          <div className="skel" style={{ width: 180, height: 14, marginBottom: 12 }} />
          <div className="skel" style={{ width: 220, height: 30, marginBottom: 10 }} />
          <div className="skel" style={{ width: 560, maxWidth: "100%", height: 18 }} />
        </div>
      </div>
      <nav className="subtabs" aria-label="People">
        {[120, 150, 90].map((width) => (
          <span key={width} className="skel" style={{ width, height: 34 }} aria-hidden="true" />
        ))}
      </nav>
      <form className="card" style={{ padding: 12, marginBottom: 14 }} aria-busy="true">
        <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
          {[220, 150, 170, 130, 90].map((width) => (
            <div key={width} className="fld" style={{ minWidth: width }}>
              <div className="skel" style={{ width: Math.min(width, 110), height: 12, marginBottom: 8 }} />
              <div className="skel" style={{ width, height: 38 }} />
            </div>
          ))}
        </div>
      </form>
      <section className="card" aria-busy="true">
        <div className="hd">
          <Users className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <div className="skel" style={{ width: 170, height: 22 }} />
          <div className="sp" style={{ flex: 1 }} />
          <div className="skel" style={{ width: 120, height: 34 }} />
        </div>
        <div className="bd">
          {Array.from({ length: 8 }, (_, index) => (
            <div key={index} className="skel" style={{ height: 34, marginBottom: 10 }} />
          ))}
        </div>
      </section>
    </div>
  );
}

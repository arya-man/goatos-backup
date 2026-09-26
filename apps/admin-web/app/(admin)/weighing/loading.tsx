import { PageHeaderSkeleton } from "@/components/app/page-header";
import { Scale } from "lucide-react";

export default function Loading() {
  return (
    <div className="screen on">
      <PageHeaderSkeleton />
      <nav className="subtabs" aria-label="Weighing analytics loading">
        {[100, 96, 94, 104, 112, 98, 96].map((width, index) => (
          <span key={`${width}-${index}`} className="skel" style={{ width, height: 34 }} aria-hidden="true" />
        ))}
      </nav>
      <section className="card" aria-busy="true">
        <div className="hd">
          <Scale className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <div className="skel" style={{ width: 220, height: 22 }} />
          <div className="sp" style={{ flex: 1 }} />
          <div className="skel" style={{ width: 190, height: 34 }} />
        </div>
        <div className="bd">
          <div className="grid g4" style={{ marginBottom: 16 }}>
            {[128, 116, 132, 122].map((width) => (
              <div key={width} className="skel" style={{ height: 88, minWidth: width }} />
            ))}
          </div>
          {Array.from({ length: 6 }, (_, index) => (
            <div key={index} className="skel" style={{ height: 34, marginBottom: 10 }} />
          ))}
        </div>
      </section>
    </div>
  );
}

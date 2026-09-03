import { Zap } from "lucide-react";

export default function Loading() {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="skel" style={{ width: 170, height: 14, marginBottom: 12 }} />
          <div className="skel" style={{ width: 230, height: 30, marginBottom: 10 }} />
          <div className="skel" style={{ width: 620, maxWidth: "100%", height: 18 }} />
        </div>
      </div>
      <section className="card" aria-busy="true">
        <div className="hd">
          <Zap className="ic" aria-hidden="true" />
          <div className="skel" style={{ width: 180, height: 22 }} />
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

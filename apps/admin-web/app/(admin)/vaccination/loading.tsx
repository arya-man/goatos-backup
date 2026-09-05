export default function Loading() {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="skel" style={{ width: 160, height: 14, marginBottom: 12 }} />
          <div className="skel" style={{ width: 190, height: 28, marginBottom: 10 }} />
          <div className="skel" style={{ width: 640, maxWidth: "100%", height: 18 }} />
        </div>
        <div className="sp" style={{ flex: 1 }} />
        <div className="skel" style={{ width: 160, height: 42, borderRadius: "var(--r)" }} />
      </div>
      <section className="card" style={{ marginBottom: 16 }} aria-busy="true">
        <div className="bd">
          <div className="chain" aria-hidden="true">
            {Array.from({ length: 4 }, (_, i) => (
              <div className="cstep" key={i}>
                <div className="skel" style={{ width: 64, height: 13, marginBottom: 12 }} />
                <div className="skel" style={{ width: 130, height: 20, marginBottom: 10 }} />
                <div className="skel" style={{ width: "90%", height: 15 }} />
              </div>
            ))}
          </div>
        </div>
      </section>
      <section className="card" aria-busy="true">
        <div className="hd">
          <div className="skel" style={{ width: 180, height: 22 }} />
          <div className="sp" style={{ flex: 1 }} />
          <div className="skel" style={{ width: 240, height: 16 }} />
        </div>
        <div className="bd" style={{ padding: "14px 14px 0" }}>
          <div className="skel" style={{ width: 280, maxWidth: "100%", height: 34 }} />
        </div>
        <div className="chipset" style={{ padding: "12px 14px" }}>
          {[96, 86, 116, 72, 110, 132].map((w, i) => (
            <div key={i} className="skel" style={{ width: w, height: 30, borderRadius: "var(--r-pill)" }} />
          ))}
        </div>
      </section>
    </div>
  );
}

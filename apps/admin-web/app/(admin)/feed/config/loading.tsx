export default function Loading() {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="skel" style={{ width: 150, height: 14, marginBottom: 12 }} />
          <div className="skel" style={{ width: 210, height: 30, marginBottom: 10 }} />
          <div className="skel" style={{ width: 680, maxWidth: "100%", height: 18 }} />
        </div>
      </div>
      <section className="card" aria-busy="true">
        <div className="hd">
          <div className="skel" style={{ width: 190, height: 22 }} />
          <div className="sp" style={{ flex: 1 }} />
          <div className="skel" style={{ width: 240, height: 16 }} />
        </div>
        <div className="chipset" style={{ padding: "12px 14px" }}>
          {[110, 92, 130, 96, 140, 120].map((width, index) => (
            <div key={index} className="skel" style={{ width, height: 30, borderRadius: 999 }} />
          ))}
        </div>
        <div className="bd">
          {Array.from({ length: 5 }, (_, index) => (
            <div key={index} className="skel" style={{ height: 42, marginBottom: 12 }} />
          ))}
        </div>
      </section>
    </div>
  );
}

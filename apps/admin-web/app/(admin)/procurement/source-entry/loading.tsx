export default function Loading() {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="skel" style={{ width: 180, height: 14, marginBottom: 12 }} />
          <div className="skel" style={{ width: 240, height: 30, marginBottom: 10 }} />
          <div className="skel" style={{ width: 620, maxWidth: "100%", height: 18 }} />
        </div>
      </div>
      <section className="card" aria-busy="true">
        <div className="hd">
          <div className="skel" style={{ width: 220, height: 22 }} />
          <div className="sp" style={{ flex: 1 }} />
          <div className="skel" style={{ width: 180, height: 16 }} />
        </div>
        <div className="tbar">
          <div className="skel" style={{ width: 260, height: 36 }} />
          <div className="skel" style={{ width: 120, height: 32 }} />
        </div>
        <div className="bd">
          {Array.from({ length: 7 }, (_, index) => (
            <div key={index} className="skel" style={{ height: 34, marginBottom: 10 }} />
          ))}
        </div>
      </section>
    </div>
  );
}

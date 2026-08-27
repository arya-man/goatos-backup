export default function Loading() {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="skel" style={{ width: 160, height: 14, marginBottom: 12 }} />
          <div className="skel" style={{ width: 230, height: 30, marginBottom: 10 }} />
          <div className="skel" style={{ width: 620, maxWidth: "100%", height: 18 }} />
        </div>
      </div>
      <div className="g4">
        {[1, 2, 3, 4].map((item) => (
          <section key={item} className="card" aria-busy="true">
            <div className="bd">
              <div className="skel" style={{ width: 90, height: 14, marginBottom: 14 }} />
              <div className="skel" style={{ width: 140, height: 30, marginBottom: 10 }} />
              <div className="skel" style={{ width: "80%", height: 15 }} />
            </div>
          </section>
        ))}
      </div>
      <section className="card" aria-busy="true">
        <div className="bd">
          {Array.from({ length: 6 }, (_, index) => (
            <div key={index} className="skel" style={{ height: 36, marginBottom: 10 }} />
          ))}
        </div>
      </section>
    </div>
  );
}

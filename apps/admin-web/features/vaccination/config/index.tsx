import { ScheduleBuilder } from "./schedule-builder";
import { VaccinationTabs } from "../vaccination-tabs";

export function ConfigSchedulePage() {
  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            PHC · <b>Vaccination</b>
          </div>
          <h1>Config — Schedule Builder</h1>
          <div className="sub">
            What should happen. Author + publish the medical schedule; obligations, SOP tasks and adherence all flow
            from PUBLISHED, source-backed rules. Drafts never generate live work.
          </div>
        </div>
      </div>

      <VaccinationTabs current="config" />

      <div className="alert warn" style={{ marginBottom: 14 }}>
        <span aria-hidden className="ic">
          ⚠
        </span>
        <div>
          Real business/medical config — <b>not public</b>. Only <b>CEO/COO publish</b>; field / verifier / park never
          see raw config (they get generated obligations + SOP tasks only). No vaccine values are invented — a draft
          publishes only when source-backed (vaccinations_db / phc / vet, reviewed, approved).
        </div>
      </div>

      <ScheduleBuilder />
    </div>
  );
}

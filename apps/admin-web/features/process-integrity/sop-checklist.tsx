import Step from "@mui/material/Step";
import StepContent from "@mui/material/StepContent";
import StepLabel from "@mui/material/StepLabel";
import Stepper from "@mui/material/Stepper";
import Typography from "@mui/material/Typography";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";
import type { SopStep } from "@/features/preventive-care-vaccination";

// SopChecklist renders an SOP's procedure steps as the mock's drawer checklist (numbered/checked circles,
// step description, "video proof required" pill) — NOT the obligation lifecycle chain (that is the
// Workflow record's stepper). `doneThrough` is derived from the obligation's computed states: steps below
// it are done, the step at it is current. Template anatomy: MUI vertical Stepper with every step expanded.
export function SopChecklist({ steps, doneThrough }: { steps: SopStep[]; doneThrough: number }) {
  return (
    <Stepper activeStep={doneThrough} orientation="vertical" data-testid="sop-checklist">
      {steps.map((s, i) => (
        <Step key={s.title} completed={i < doneThrough} expanded>
          <StepLabel>
            <Typography variant="subtitle2" component="span">{s.title}</Typography>
          </StepLabel>
          <StepContent>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>{s.detail}</Typography>
            {s.videoProof ? (
              <Label color="secondary" sx={{ mt: 1 }} startIcon={<Iconify icon="solar:videocamera-record-bold" />}>
                video proof required
              </Label>
            ) : null}
          </StepContent>
        </Step>
      ))}
    </Stepper>
  );
}

import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { DividedStack } from "@/components/app/divided-stack";
import { getVerificationSampling } from "@/lib/api/server";
import { setVerificationSamplingPoliciesAction } from "./randomization-actions";
import { InfoHint } from "@/components/app/info-hint";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Button from "@mui/material/Button";
import TextField from "@mui/material/TextField";
import CardHeader from "@mui/material/CardHeader";
import LinearProgress from "@mui/material/LinearProgress";
import { Label } from "@/components/minimal/label";
import { EmptyContent } from "@/components/minimal/empty-content";

/**
 * RANDOMIZATION: per module, the share of that module's proof videos the verifier actually has to
 * watch, and today's progress against that share (maintainer decision 2026-08-26).
 *
 * Gating: the caller MUST check `controlEnabled(pageContract, "randomization", false)` before
 * rendering this component (see verification-review-page.tsx) -- the SAME capability
 * (permissions.VerificationSampling) gates both the page contract control and the backend endpoint
 * this component reads, so a verifier's -- or a director's -- build never even calls the endpoint.
 * This component reads the contract for copy only, never for its own gating decision.
 *
 * EVERY NUMBER HERE IS SERVER-COMPUTED, including progress. A share of 40% fully reviewed reads
 * 100%, and that arithmetic lives in the backend so the panel, a future phone screen and any report
 * cannot each derive a different completion number for the same day.
 *
 * Every visible word is backend copy: the module and page labels come from the verification
 * registry, and a locked row's reason is composed by the backend. The category token that
 * identifies a row is submitted in a hidden field and never printed -- it is config vocabulary, and
 * the copy firewall bans it from visible UI.
 */
export async function Randomization({
  pageContract,
  returnTo,
}: {
  pageContract: AdminUiPageContract;
  /** The live page URL, so a save returns to the queue the CEO was looking at. */
  returnTo: string;
}) {
  const result = await getVerificationSampling({});
  if (!result.ok) {
    return <EmptyContent title={copy(pageContract, "randomization.unavailable")} sx={{ py: 5 }} />;
  }
  const { categories, business_date: businessDate } = result.data;

  const editable = categories.some((row) => row.waivable);
  // Template card anatomy (EcommerceSalesOverview progress rows inside one Card with a CardHeader
  // action): one row per module, dashed dividers between rows, the save as the header action.
  return (
    <form action={setVerificationSamplingPoliciesAction}>
      <input type="hidden" name="return_to" value={returnTo} />
      {/* Part of every save's idempotency identity -- see the action. */}
      <input type="hidden" name="business_date" value={businessDate} />
      <Card>
        <CardHeader
          title={
            <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
              {copy(pageContract, "randomization.col.share")}
              <InfoHint text={copy(pageContract, "randomization.share_help")} />
            </Box>
          }
          action={
            editable ? (
              <Button type="submit" variant="contained" color="primary">
                {copy(pageContract, "randomization.apply")}
              </Button>
            ) : null
          }
        />
        <DividedStack flexItem={false} sx={{ gap: 3, px: 3, py: 3 }}>
          {categories.map((row) => {
            // Captured, selected and reviewed are NOT disjoint -- selected is a subset of captured,
            // reviewed a subset of selected -- so they are read out separately and never summed.
            const share = row.waivable ? `${row.sample_percent}%` : copy(pageContract, "randomization.locked");
            const done = row.progress_percent >= 100;
            return (
              <div key={row.category}>
                <Box sx={{ mb: 1, gap: 0.5, display: "flex", alignItems: "center", typography: "subtitle2" }}>
                  <Box component="span" sx={{ flexGrow: 1, minWidth: 0 }}>
                    {row.module_label} · {row.page_label}
                  </Box>
                  <Label variant="soft" color={row.waivable ? "primary" : "default"}>
                    {share}
                  </Label>
                </Box>

                {/* Progress against HER SHARE, not against the day's capture. The bar is the number
                    the maintainer asked for: at 40% sampling, all 40% reviewed fills it. */}
                <LinearProgress
                  variant="determinate"
                  color={done ? "success" : "primary"}
                  value={Math.min(100, Math.max(0, row.progress_percent))}
                  sx={{ bgcolor: "action.selected" }}
                />
                <Box sx={{ mt: 1, typography: "body2", color: "text.secondary" }}>
                  {done
                    ? copy(pageContract, "randomization.complete")
                    : `${row.reviewed}/${row.selected} ${copy(pageContract, "randomization.reviewed")}`}
                </Box>

                <Box sx={{ mt: 1, display: "flex", flexWrap: "wrap", gap: 0.5 }}>
                  <Label variant="soft">
                    {row.captured} {copy(pageContract, "randomization.videos_arrived")}
                  </Label>
                  <Label variant="soft" color="info">
                    {row.selected} {copy(pageContract, "randomization.to_review")}
                  </Label>
                  {row.auto_accepted > 0 ? (
                    <Label variant="soft" color="success">
                      {row.auto_accepted} {copy(pageContract, "randomization.settled")}
                    </Label>
                  ) : null}
                </Box>

                {row.waivable ? (
                  <Box sx={{ mt: 2 }}>
                    <input type="hidden" name={`current__${row.category}`} value={row.sample_percent} />
                    {/* defaultValue, never value: an uncontrolled field in a server-action form.
                        No client-side clamp, and blank is NOT coerced to 0 -- the sheet's action
                        refuses it and the backend owns the range. */}
                    <TextField
                      type="number"
                      size="small"
                      fullWidth
                      name={`sample_percent__${row.category}`}
                      label={copy(pageContract, "randomization.col.share")}
                      defaultValue={row.sample_percent}
                      slotProps={{ htmlInput: { min: 0, max: 100, step: 1, inputMode: "numeric" } }}
                    />
                  </Box>
                ) : (
                  <Box sx={{ mt: 1, display: "inline-flex", alignItems: "center", gap: 0.5, typography: "body2", color: "text.secondary" }}>
                    {copy(pageContract, "randomization.locked")}
                    {row.locked_reason ? <InfoHint text={row.locked_reason} /> : null}
                  </Box>
                )}

                {row.effective_from ? (
                  <Box sx={{ mt: 1, typography: "caption", color: "text.disabled" }}>
                    {copy(pageContract, "randomization.effective")} {row.effective_from}
                    {row.set_by_name ? ` · ${copy(pageContract, "randomization.set_by")} ${row.set_by_name}` : ""}
                  </Box>
                ) : null}
              </div>
            );
          })}
        </DividedStack>
      </Card>
    </form>
  );
}

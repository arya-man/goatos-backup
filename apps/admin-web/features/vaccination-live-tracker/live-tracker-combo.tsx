import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import MuiLink from "@mui/material/Link";
import Table from "@mui/material/Table";
import TableBody from "@mui/material/TableBody";
import TableCell from "@mui/material/TableCell";
import TableHead from "@mui/material/TableHead";
import TableRow from "@mui/material/TableRow";
import Typography from "@mui/material/Typography";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { Tag, type Tone } from "@/components/ui-primitives";
import {
  copy,
  optionLabel,
  optionTone,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import type { LiveTrackerCombo } from "@/lib/api/vaccination-live-tracker";
import { LiveEmpty, LiveHeadRow } from "./live-ui";
import { TAP_MIN } from "@/theme/tap-target";

// Combo doses — one proof, multiple same-day obligations.
//
// The animal cell renders the REAL scanned identifier. The mock's `GT-#####` ids were generated, and
// reproducing that pattern would put a fabricated animal number in front of a farm director; where a
// goat is dual-tagged the second tag is carried in the cell's title rather than invented into a
// column that has nowhere to go.
export function LiveTrackerComboCard({
  combo,
  passportHref,
  pageContract,
}: {
  combo: LiveTrackerCombo;
  passportHref: (goatId: string) => string;
  pageContract: AdminUiPageContract;
}) {
  const placeholder = copy(pageContract, "label.placeholder");
  const head = [
    copy(pageContract, "section.combo.header_animal"),
    copy(pageContract, "section.combo.header_shed"),
    copy(pageContract, "section.combo.header_proof"),
    copy(pageContract, "section.combo.header_doses"),
  ];
  return (
    <Card id="lt-combo" sx={{ scrollMarginTop: 80 }}>
      <CardHeader
        title={copy(pageContract, "section.combo.title")}
        subheader={
          // ONE Label per distinct antigen actually present, never their union joined into a single
          // label: real combos are variable-N and a day can carry several different ones, so joining
          // every label produced a combination no animal received.
          combo.vaccine_labels.length ? (
            <Box component="span" sx={{ mt: 1, display: "flex", flexWrap: "wrap", gap: 0.75 }}>
              {combo.vaccine_labels.map((label) => (
                <Label key={label} variant="soft" color="secondary">
                  {label}
                </Label>
              ))}
            </Box>
          ) : null
        }
        action={
          <Typography variant="caption" sx={{ color: "text.secondary", whiteSpace: "nowrap" }}>
            {combo.animal_count}{" "}
            {combo.animal_count === 1 ? copy(pageContract, "section.combo.count_suffix_one") : copy(pageContract, "section.combo.count_suffix")}
          </Typography>
        }
        slotProps={{ subheader: { component: "div" } }}
        sx={{ mb: 2 }}
      />

      {combo.rows.length === 0 ? (
        <LiveEmpty title={copy(pageContract, "section.combo.empty_title")} body={copy(pageContract, "section.combo.empty_body")} />
      ) : (
        <Scrollbar>
          <Table sx={{ minWidth: 640 }} aria-label={copy(pageContract, "section.combo.title")}>
            <TableHead>
              <LiveHeadRow labels={head} />
            </TableHead>
            <TableBody>
              {combo.rows.map((row) => {
                const dualTagTitle = row.secondary_tag ? `${row.primary_tag} · ${row.secondary_tag}` : row.primary_tag;
                return (
                  <TableRow hover key={row.goat_id}>
                    <TableCell>
                      <MuiLink
                        component={LocalOverlayLink}
                        href={passportHref(row.goat_id)}
                        scroll={false}
                        underline="hover"
                        sx={{ minHeight: TAP_MIN, display: "inline-flex", alignItems: "center", typography: "subtitle2", fontFamily: "monospace" }}
                        title={dualTagTitle || row.display_id}
                        aria-label={`${copy(pageContract, "drawer.passport.aria")} — ${row.display_id || row.primary_tag}`}
                      >
                        {row.primary_tag || row.display_id || placeholder}
                      </MuiLink>
                    </TableCell>
                    <TableCell>{row.shed_label || placeholder}</TableCell>
                    <TableCell>
                      <Tag tone={optionTone(pageContract, "live_proof_state", row.proof_state) as Tone}>
                        {optionLabel(pageContract, "live_proof_state", row.proof_state)}
                      </Tag>
                    </TableCell>
                    <TableCell>
                      <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
                        {row.doses.map((dose) => (
                          <Tag key={dose.obligation_id} tone={optionTone(pageContract, "live_dose_state", dose.state) as Tone}>
                            {dose.vaccine_label} · {optionLabel(pageContract, "live_dose_state", dose.state)}
                          </Tag>
                        ))}
                      </Box>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </Scrollbar>
      )}

      {/* The all-combo-animals control implies a longer list behind the card. No paginated
          combo-animal endpoint or route exists on this surface, so BOTH branches are disabled with a
          visible reason — never pointed at rows[0]'s passport drawer. */}
      <Box sx={{ p: 2, display: "flex", alignItems: "center", gap: 1.5, flexWrap: "wrap", borderTop: 1, borderColor: "divider", borderTopStyle: "dashed" }}>
        <Button
          size="small"
          color="inherit"
          disabled
          title={combo.rows_truncated ? copy(pageContract, "section.combo.truncated_reason") : copy(pageContract, "section.combo.all_listed")}
        >
          {copy(pageContract, "action.all_combo_animals")}
        </Button>
        {combo.rows_truncated ? (
          <Typography variant="caption" data-truncnote="" sx={{ color: "text.secondary" }}>
            {copy(pageContract, "section.combo.truncated_reason")}
          </Typography>
        ) : null}
      </Box>
    </Card>
  );
}

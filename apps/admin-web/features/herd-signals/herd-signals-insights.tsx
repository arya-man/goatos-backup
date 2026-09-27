import type { HerdInsightCard, HerdSignalType } from "@/lib/api/herd-signals";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";
import { EmptyState } from "@/components/app/empty-state";
import { Iconify, type IconifyName } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";

const TYPE_LABEL: Record<HerdSignalType, string> = {
  direct: "Direct",
  derived: "Derived",
  correlated: "Correlated",
  inferred: "Inferred",
};

// The backend now ports the label (Title Case), formula (plain-English, not SQL) and signal_type
// (badge tier) verbatim from the design reference's card set card-for-card (see
// backend/internal/herdsignals/app/service.go GetInsights) -- this component renders those fields
// as returned, with no frontend copy override, so backend and screen can never drift.
//
// One icon per PROVENANCE tier, not per card key. The mock gives each of its sample cards its own
// glyph; our card list is backend-owned and open-ended, so keying the glyph off `signal_type` is
// the only mapping that stays honest when the backend adds a card this file has never seen.
const TYPE_COLOR: Record<HerdSignalType, "success" | "info" | "warning" | "secondary"> = {
  direct: "success",
  derived: "info",
  correlated: "warning",
  inferred: "secondary",
};

const TYPE_ICON: Record<HerdSignalType, IconifyName> = {
  direct: "solar:check-circle-bold",
  derived: "solar:chart-square-outline",
  correlated: "solar:atom-outline",
  inferred: "solar:info-circle-bold",
};

export function HerdSignalsInsights({ cards }: { cards: HerdInsightCard[] }) {
  return (
    <Stack spacing={3}>
      {/* Honesty disclaimer + reading guide (9384d08f4 / 1f0b71be9): what the tag can and cannot detect. */}
      <Alert severity="info">
        <b>What the tag actually reports:</b> tag ID, BLE MAC, RSSI, battery mV, tag temperature, a
        cumulative motion counter, sensor-OK bits, gateway ID and timestamps. It does not detect eating,
        rumination, posture, walking, fever, body temperature or disease. Everything below is labelled
        Direct, Derived, Correlated or Inferred.
      </Alert>
      <Alert severity="success">
        <b>How to read this:</b> use Live Monitor and Alerts for animal-level action. These cards explain
        fleet health: tag coverage, missing signals, weak radio, low battery and whether movement is
        merely correlated with farm events.
      </Alert>
      {cards.length === 0 ? (
        <EmptyState
          icon={<svg className="ic" viewBox="0 0 24 24">
            <path d="M3 12h4l3 8 4-16 3 8h4" />
          </svg>}
          title="No insight cards yet"
          description="Insights are computed from activity windows written as packets arrive."
        />
      ) : (
        // Template widget-summary cards (app / ecommerce overview): title, big figure, caption.
        <Grid container spacing={3}>
          {cards.map((card) => (
            <Grid key={card.key} size={{ xs: 12, sm: 6, md: 4 }}>
              <Card sx={{ p: 3, height: 1, display: "flex", flexDirection: "column", gap: 1.5 }}>
                <Box sx={{ display: "flex", alignItems: "center", gap: 1 }}>
                  <Typography variant="subtitle2" sx={{ flex: 1, minWidth: 0 }}>
                    {card.label}
                  </Typography>
                  <Label variant="soft" color={TYPE_COLOR[card.signal_type]} startIcon={<Iconify icon={TYPE_ICON[card.signal_type]} />}>
                    {TYPE_LABEL[card.signal_type]}
                  </Label>
                </Box>
                {/* An uncomputed card renders "—", never a 0 that reads as a measured count. */}
                <Typography variant="h3">
                  {card.value ?? "—"}
                  {card.value !== null && card.unit ? (
                    <Box component="span" sx={{ ml: 0.5, typography: "subtitle2", color: "text.secondary" }}>
                      {card.unit}
                    </Box>
                  ) : null}
                </Typography>
                <Typography variant="caption" sx={{ color: "text.secondary" }}>
                  {card.formula}
                </Typography>
                {card.caveat ? (
                  <Typography variant="caption" sx={{ color: "text.disabled" }}>
                    {card.caveat}
                  </Typography>
                ) : null}
              </Card>
            </Grid>
          ))}
        </Grid>
      )}
    </Stack>
  );
}

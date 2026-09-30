import type { SxProps, Theme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";

/**
 * The single-person AssigneePicker (`components/assignee-picker.tsx`, `mode="single"`) drawn as a
 * template outlined field inside a form: the trigger reads like the TextFields beside it (56px,
 * the template input radius and grey-500 20% border, primary ring when open / focused, error
 * border when the host's required check failed). Put it on the Box that holds the picker. Used by
 * the /tasks New task dialog ("For") and the routine drawer ("Who does it"); it replaced the
 * `.lt-modal` / `.prt` `.avs-*` rules in mesha-theme.css (deleted).
 */
export const ASSIGNEE_FIELD_SX: SxProps<Theme> = (theme) => ({
  "& .avs-single": { position: "relative", display: "grid", gridTemplateColumns: "1fr", width: 1 },
  "& .avs-trigger": {
    display: "flex",
    alignItems: "center",
    gap: 1.25,
    width: 1,
    minHeight: "var(--input-h)",
    px: 1.75,
    py: 1,
    m: 0,
    bgcolor: "transparent",
    border: `1px solid ${varAlpha(theme.vars.palette.grey["500Channel"], 0.2)}`,
    borderRadius: "var(--r-md)",
    font: "inherit",
    typography: "body1",
    color: "text.primary",
    textAlign: "left",
    cursor: "pointer",
    transition: theme.transitions.create(["border-color", "box-shadow"], { duration: theme.transitions.duration.shorter }),
    "&:hover": { borderColor: "text.primary" },
    "&.on, &:focus-visible": { outline: "none", borderColor: "primary.main", boxShadow: `inset 0 0 0 1px ${theme.vars.palette.primary.main}` },
    "&[aria-invalid='true']": { borderColor: "error.main" },
    "& .avs-value": { flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
    "& .muted": { color: "text.secondary" },
    "& svg": { width: "var(--sp-2)", height: "var(--sp-2)", flex: "none", color: "text.secondary" },
  },
});

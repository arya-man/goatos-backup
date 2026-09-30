// Ask Mesha brand marks: the Twemoji goat (launcher, dock, in-progress send) and the Mesha मे
// logo (panel header, answer avatar). Colours are theme palette tokens; the goat's bob / trot
// motion is an MUI keyframe in sx (reduced motion turns it off). The rest of the assistant's look
// lives below as theme sx on each part (one export per part, picked by state; no stylesheet).
//
// Goat mascot: Twitter Twemoji goat (U+1F410), © Twitter, licensed CC-BY 4.0
// (https://github.com/twitter/twemoji). A real, recognizable side-profile goat
// used for the launcher, header, and message avatars, and (with a trot bob) for
// the in-progress send button. Attribution retained per the CC-BY 4.0 license;
// see docs/ceo-ai/access-policy.md / mascot note.

import type { ReactElement } from "react";
import Box from "@mui/material/Box";
import { keyframes, type SxProps, type Theme } from "@mui/material/styles";
import { varAlpha } from "minimal-shared/utils";

// The Twemoji goat artwork, shared by the avatar and the walking-button variant.
function GoatArt(): ReactElement {
  return (
    <>
      <path fill="var(--palette-warning-main)" d="M7.44 7.503c-1-4 3.687-6 8-4 .907.421.948 1.316 0 1-3-1-6 1-4 4 1.109 1.664-3.233 2.068-4-1z" />
      <path fill="var(--palette-warning-light)" d="M6.136 5.785c-1-4 3.687-6 8-4 .907.421.949 1.316 0 1-3-1-6 1-4 4 1.11 1.664-3.233 2.067-4-1z" />
      <path fill="var(--palette-grey-200)" d="M5 14.785c0 4-2 4.827-2 4 0-2-1 0-1-1v-3c0-1.657.671-3 1.5-3s1.5 1.343 1.5 3z" />
      <path fill="var(--palette-grey-300)" d="M35.159 10.49c-.68-1.643-2.313-2.705-4.159-2.705-.553 0-1 .448-1 1s.447 1 1 1c1.034 0 1.941.577 2.312 1.471.341.824.168 1.758-.455 2.647-.984-1.506-2.602-2.618-4.856-2.618-2.391 0-7.279.714-10.828 1.289-.052-.094-.105-.188-.172-.289-2-3-4-8.157-7-8.157-4 0-10 4.986-10 9.157 0 2.544 5.738 2.929 7.486 2.988.697 1.43 1.414 2.934 2.232 4.33.066.205.155.429.282.683 3 6 3.119 14.5 4.5 14.5s2.5-4.857 2.5-9c0-.151-.004-.299-.007-.447 3.126.649 6.607.322 9.677-.61 1.448 5.045 1.77 10.058 2.83 10.058 1.342 0 2.433-8.818 2.494-13.12C33.316 21.226 34 19.51 34 17.785c0-.605-.086-1.23-.248-1.843 1.614-1.644 2.143-3.676 1.407-5.452z" />
      <circle fill="var(--palette-grey-800)" cx="7" cy="9.285" r="1" />
    </>
  );
}

const motion = (name: string, duration: string) => ({
  transformOrigin: "18px 30px",
  "@media (prefers-reduced-motion: no-preference)": { animation: `${name} ${duration} ease-in-out infinite` },
});

/** The goat mark; `size` is its box (the launcher 28, the header dock a spacing token). */
export function GoatAvatar({ size = 28 }: { size?: number | string }): ReactElement {
  return (
    <Box
      component="svg"
      viewBox="0 0 36 36"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="Mesha goat assistant"
      sx={{ width: size, height: size, display: "block", flex: "none" }}
    >
      <Box component="g" sx={motion(goatBob.toString(), "2.6s")}>
        <GoatArt />
      </Box>
    </Box>
  );
}

export function GoatWalking({ size = 18 }: { size?: number | string }): ReactElement {
  return (
    <Box
      component="svg"
      viewBox="0 0 36 36"
      xmlns="http://www.w3.org/2000/svg"
      role="img"
      aria-label="Generating answer"
      sx={{ width: size, height: size, display: "block", flex: "none" }}
    >
      <Box component="g" sx={motion(goatTrot.toString(), "0.7s")}>
        <GoatArt />
      </Box>
    </Box>
  );
}

// Mesha logo component for panel header and message avatars.
// Renders the brand मे mark exactly like the app-shell sidebar logo
// (components/mesha-shell.tsx `.brand .logo`): the मे glyph in a green-soft disc
// with a brand-green ring and bold brand-green glyph. Kept identical to the
// sidebar so the Ask Mesha mark matches the main Mesha logo pixel-for-pixel at
// any size (a stale PNG avatar previously diverged from the sidebar mark).
export function MeshaLogo(props: { width?: number; height?: number; className?: string }): ReactElement {
  const { width = 26, className } = props;
  return (
    <Box
      component="span"
      aria-label="Mesha"
      role="img"
      className={className}
      sx={{
        width,
        height: width,
        flex: "none",
        display: "grid",
        placeItems: "center",
        borderRadius: "50%",
        border: "1.5px solid",
        borderColor: "primary.main",
        bgcolor: "primary.main",
        color: "primary.contrastText",
        fontWeight: 800,
        fontSize: Math.round(width * 0.47),
        lineHeight: 1,
        fontFamily: "inherit",
      }}
    >
      मे
    </Box>
  );
}

// Theme sx for the Ask Mesha assistant (panel, messages, markdown, attachments, lightbox, watch
// card). Every colour is a theme palette token (theme.vars.palette + varAlpha) so both colour schemes
// follow the Mesha palette; sizes are spacing units or app/minimal-tokens.css tokens. The phone layout
// is theme.breakpoints.down("sm"). There is no stylesheet: parts pick their sx from here by state.
export type Sx = SxProps<Theme>;
type Styles = Record<string, unknown>;
const sx = (f: (t: Theme) => Styles): Sx => f as unknown as Sx;
const phone = (t: Theme) => t.breakpoints.down("sm");
const up = (t: Theme) => t.breakpoints.up("sm");

// ---- colour helpers --------------------------------------------------------------------------
const line = (t: Theme) => t.vars.palette.divider;
const soft = (t: Theme) => varAlpha(t.vars.palette.primary.mainChannel, 0.16);
const hover = (t: Theme) => varAlpha(t.vars.palette.grey["500Channel"], 0.08);
const greyA = (t: Theme, a: number) => varAlpha(t.vars.palette.grey["500Channel"], a);
const blackA = (t: Theme, a: number) => varAlpha(t.vars.palette.common.blackChannel, a);
const whiteA = (t: Theme, a: number) => varAlpha(t.vars.palette.common.whiteChannel, a);
const MONO = "ui-monospace,SFMono-Regular,Menlo,monospace";
const EASE = "cubic-bezier(.34,.1,.64,.9)";
const REDUCED = "@media (prefers-reduced-motion: reduce)";

// ---- font sizes (type-scale tokens) ----------------------------------------------------------
const FS = {
  xxs: "calc(var(--fs-caption) - 2px)", // 10
  xs: "calc(var(--fs-caption) - 1px)", // 11
  xsh: "calc(var(--fs-caption) - 0.5px)", // 11.5
  sm: "var(--fs-caption)", // 12
  smh: "calc(var(--fs-caption) + 0.5px)", // 12.5
  md: "var(--fs-button-sm)", // 13
  mdh: "calc(var(--fs-body2) - 0.5px)", // 13.5
  body: "var(--fs-body2)", // 14
  lg: "calc(var(--fs-body2) + 1px)", // 15
  input: "var(--fs-body1)", // 16
};

// ---- keyframes -------------------------------------------------------------------------------
export const goatBob = keyframes`0%,100%{transform:translateY(0)}50%{transform:translateY(-1.5px)}`;
export const goatTrot = keyframes`0%,100%{transform:translateY(0) rotate(0deg)}25%{transform:translateY(-1px) rotate(-2deg)}50%{transform:translateY(0) rotate(0deg)}75%{transform:translateY(-1px) rotate(2deg)}`;
const pulse = keyframes`50%{opacity:.7}`;
const panelOpen = keyframes`from{opacity:0;transform:scale(.8) translateY(8px)}to{opacity:1;transform:scale(1) translateY(0)}`;
const caretBlink = keyframes`0%,49%{opacity:1}50%,100%{opacity:0}`;
const slideUp = keyframes`from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:translateY(0)}`;
const bounce = keyframes`0%,100%{opacity:.4;transform:translateY(0)}50%{opacity:1;transform:translateY(-4px)}`;
const shimmer = keyframes`from{background-position:100% 0}to{background-position:-100% 0}`;
const spin = keyframes`to{transform:rotate(360deg)}`;
const watchPulse = keyframes`to{box-shadow:0 0 0 8px transparent}`;

export type View = "normal" | "min" | "max";

// ---- panel shell -----------------------------------------------------------------------------
export const panelSx = (view: View): Sx =>
  sx((t) => ({
    position: "relative",
    display: "flex",
    flexDirection: "column",
    height: view === "min" ? "auto" : "100%",
    bgcolor: "background.paper",
    border: `1px solid ${line(t)}`,
    borderRadius: "calc(var(--r-xl) + var(--sp-half))",
    overflow: "hidden",
    boxShadow: t.vars.customShadows.z24,
    animation: `${panelOpen} .3s ${EASE}`,
    overscrollBehavior: "contain",
    [phone(t)]:
      view === "min"
        ? { height: "auto", borderRadius: 0, border: 0, borderTop: `1px solid ${line(t)}`, pt: 0, pb: "env(safe-area-inset-bottom)" }
        : { height: "100dvh", borderRadius: 0, border: 0, pt: "env(safe-area-inset-top)", pb: "env(safe-area-inset-bottom)" },
  }));

export const dropSx: Sx = sx((t) => ({
  position: "absolute",
  inset: 8,
  zIndex: 5,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  border: `2px dashed ${t.vars.palette.primary.main}`,
  borderRadius: "var(--r-xl)",
  bgcolor: varAlpha(t.vars.palette.grey["900Channel"], 0.82),
  color: "common.white",
  fontSize: FS.lg,
  fontWeight: 600,
  pointerEvents: "none",
}));

export const headSx = (view: View): Sx =>
  sx((t) => ({
    display: "flex",
    alignItems: "center",
    gap: 1.5,
    py: 1.75,
    px: 2,
    borderBottom: view === "min" ? 0 : `1px solid ${line(t)}`,
    cursor: view === "min" ? "pointer" : undefined,
    background: `linear-gradient(135deg, ${t.vars.palette.background.paper} 0%, ${t.vars.palette.background.neutral} 100%)`,
    [phone(t)]: { py: 1.25, px: 1.5, gap: 1 },
  }));

/** Header icon button (threads toggle, minimise, maximise, close). */
export const headIconSx = (hideOnPhone = false): Sx =>
  sx((t) => ({
    width: 36,
    height: "var(--btn-h)",
    borderRadius: "var(--r2)",
    border: `1px solid ${line(t)}`,
    bgcolor: "background.default",
    color: "text.primary",
    transition: "all .15s ease",
    "&:hover": { bgcolor: hover(t), borderColor: "primary.light" },
    "&:active": { transform: "scale(.95)" },
    '&[aria-pressed="true"]': { bgcolor: soft(t), borderColor: "primary.main", color: "primary.dark" },
    [phone(t)]: { width: 44, height: "var(--tap-min)", ...(hideOnPhone ? { display: "none" } : null) },
  }));

export const markSx: Sx = { display: "flex", alignItems: "center", justifyContent: "center", flex: "none" };

export const headTextSx: Sx = sx((t) => ({
  display: "flex",
  flexDirection: "column",
  minWidth: 0,
  flex: 1,
  "& > b": { fontSize: FS.body, fontWeight: 600, color: "text.primary", lineHeight: 1.2 },
  "& > small": {
    fontSize: FS.xsh,
    color: "text.secondary",
    mt: 0.25,
    whiteSpace: "nowrap",
    overflow: "hidden",
    textOverflow: "ellipsis",
    [phone(t)]: { display: "none" },
  },
}));

export const headButtonsSx: Sx = sx((t) => ({ display: "flex", alignItems: "center", gap: 0.75, [phone(t)]: { gap: 0.5 } }));

export const bodySx = (view: View): Sx =>
  sx((t) => ({ display: view === "min" ? "none" : "flex", flex: 1, minHeight: 0, [phone(t)]: { position: "relative" } }));

export const scrimSx: Sx = sx((t) => ({
  display: "none",
  [phone(t)]: { display: "block", position: "absolute", inset: 0, zIndex: 1, border: 0, p: 0, bgcolor: blackA(t, 0.45) },
}));

// ---- chats list ------------------------------------------------------------------------------
export const sideSx = (view: View, shown: boolean): Sx =>
  sx((t) => ({
    width: 200,
    flex: "none",
    borderRight: `1px solid ${line(t)}`,
    bgcolor: "background.neutral",
    display: shown ? "flex" : "none",
    flexDirection: "column",
    minHeight: 0,
    [up(t)]: view === "normal" ? { width: 176 } : {},
    [phone(t)]: {
      position: "absolute",
      inset: "0 auto 0 0",
      width: "min(78vw, calc(var(--sp-5) * 7))",
      zIndex: 2,
      boxShadow: t.vars.customShadows.z16,
    },
  }));

export const sideHeadSx: Sx = {
  display: "flex",
  alignItems: "center",
  justifyContent: "space-between",
  pt: 1.5,
  px: 1.5,
  pb: 1.25,
  "& > span": { fontSize: FS.xxs, fontWeight: 700, letterSpacing: ".08em", textTransform: "uppercase", color: "text.secondary" },
};

export const newChatSx: Sx = sx((t) => ({
  display: "flex",
  alignItems: "center",
  gap: 0.625,
  border: `1px solid ${varAlpha(t.vars.palette.primary.mainChannel, 0.55)}`,
  bgcolor: "transparent",
  color: "primary.main",
  borderRadius: "var(--r-md)",
  py: 0.75,
  px: 1.125,
  fontSize: FS.xs,
  fontWeight: 600,
  whiteSpace: "nowrap",
  transition: "all .15s ease",
  "&:hover": { transform: "translateY(-1px)", bgcolor: soft(t), boxShadow: t.vars.customShadows.z4 },
}));

export const threadsSx: Sx = { flex: 1, overflowY: "auto", pt: 0.75, px: 1, pb: 1.25, overscrollBehavior: "contain" };

export const threadSx = (selected: boolean, confirming: boolean): Sx =>
  sx((t) => ({
    display: "flex",
    alignItems: "center",
    gap: 0.75,
    borderRadius: "var(--r2)",
    p: 1,
    cursor: confirming ? "default" : "pointer",
    transition: "all .12s ease",
    "&:hover": { bgcolor: hover(t) },
    "&:hover [data-thread-act]": { opacity: 1 },
    "@media (hover: hover)": {
      "&:not(:hover):not(:focus-within) [data-thread-act]": { width: 0, ml: -0.75, overflow: "hidden" },
    },
    ...(selected ? { bgcolor: soft(t), borderLeft: `3px solid ${t.vars.palette.primary.main}` } : null),
    ...(confirming ? { bgcolor: varAlpha(t.vars.palette.error.mainChannel, 0.12), "&:hover": { bgcolor: varAlpha(t.vars.palette.error.mainChannel, 0.12) } } : null),
  }));

export const threadTitleSx: Sx = {
  flex: 1,
  minWidth: 0,
  fontSize: FS.sm,
  color: "text.primary",
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

export const threadRenameSx: Sx = sx((t) => ({
  flex: 1,
  minWidth: 0,
  font: "inherit",
  "& .MuiInputBase-input": {
    fontSize: FS.sm,
    border: `1px solid ${t.vars.palette.primary.main}`,
    borderRadius: "var(--r-md)",
    py: 0.5,
    px: 1,
    bgcolor: "background.paper",
    color: "text.primary",
  },
}));

export const threadActSx: Sx = sx(() => ({
  opacity: 0,
  width: 24,
  height: "var(--sp-3)",
  color: "text.secondary",
  borderRadius: "var(--r-sm)",
  flex: "none",
  transition: "all .12s ease",
  "&:hover": { bgcolor: "error.main", color: "error.contrastText" },
  "&:focus-visible": { opacity: 1 },
}));

export const confirmSx: Sx = sx((t) => ({
  display: "flex",
  flexWrap: "wrap",
  alignItems: "center",
  gap: 0.75,
  width: 1,
  fontSize: FS.sm,
  color: "text.primary",
  "& > span": { flex: "1 1 100%" },
  "& .MuiButtonBase-root": { fontSize: FS.xsh, py: 0.375, px: 1.125, borderRadius: "var(--r-sm)" },
  "& [data-confirm=yes]": { bgcolor: "error.main", color: "common.white" },
  "& [data-confirm=no]": { border: `1px solid ${line(t)}`, color: "text.primary" },
}));

export const sideEmptySx: Sx = { p: 1.5, fontSize: FS.sm, color: "text.secondary", lineHeight: 1.6 };

// ---- conversation ----------------------------------------------------------------------------
export const mainSx: Sx = { flex: 1, display: "flex", flexDirection: "column", minWidth: 0, minHeight: 0 };

export const logSx: Sx = sx((t) => ({
  flex: 1,
  overflowY: "auto",
  overflowX: "hidden",
  p: 2,
  display: "flex",
  flexDirection: "column",
  gap: 1.75,
  overscrollBehavior: "contain",
  [phone(t)]: { p: 1.5 },
}));

export const msgWrapSx: Sx = { display: "flex", gap: 1, alignItems: "flex-start", maxWidth: 1, minWidth: 0 };

export const avatarSx: Sx = {
  width: 32,
  height: "var(--sp-4)",
  borderRadius: "50%",
  flexShrink: 0,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  overflow: "hidden",
};

export const msgSx = (role: string, view: View): Sx =>
  sx((t) => ({
    display: "flex",
    flexDirection: "column",
    gap: 0.75,
    minWidth: 0,
    ...(role === "user"
      ? { alignSelf: "flex-end", alignItems: "flex-end", maxWidth: "85%", [phone(t)]: { maxWidth: "92%" } }
      : {
          alignSelf: "flex-start",
          alignItems: "flex-start",
          flex: "1 1 auto",
          maxWidth: view === "max" ? "min(980px, calc(100% - 40px))" : "calc(100% - 40px)",
          ...(view === "max" ? null : { [phone(t)]: { maxWidth: "calc(100% - 36px)" } }),
        }),
  }));

export const bubbleSx = (role: string, state: string): Sx =>
  sx((t) => ({
    py: 1.5,
    px: 1.75,
    borderRadius: "var(--r-xl)",
    fontSize: FS.md,
    lineHeight: 1.6,
    whiteSpace: "pre-wrap",
    wordBreak: "break-word",
    minWidth: 0,
    maxWidth: 1,
    overflowWrap: "anywhere",
    ...(role === "user"
      ? {
          bgcolor: soft(t),
          color: "text.primary",
          border: `1px solid ${varAlpha(t.vars.palette.primary.mainChannel, 0.45)}`,
          borderBottomRightRadius: "var(--sp-half)",
        }
      : {
          bgcolor: "background.neutral",
          color: "text.primary",
          border: `1px solid ${line(t)}`,
          borderBottomLeftRadius: "var(--sp-half)",
          boxShadow: t.vars.customShadows.z1,
          width: 1,
          boxSizing: "border-box",
        }),
    // Tinted, not solid red: white on the error colour fails 4.5:1 for body text.
    ...(state === "error"
      ? {
          bgcolor: `color-mix(in srgb, ${t.vars.palette.error.main} 14%, ${t.vars.palette.background.neutral})`,
          border: `1px solid ${varAlpha(t.vars.palette.error.mainChannel, 0.55)}`,
        }
      : null),
  }));

export const caretSx: Sx = {
  display: "inline-block",
  width: 6,
  height: "calc(var(--sp-2) - 2px)",
  ml: 0.25,
  bgcolor: "primary.main",
  verticalAlign: "text-bottom",
  borderRadius: "calc(var(--r-sm) / 6)",
  animation: `${caretBlink} .6s steps(1) infinite`,
};

export const msgFilesSx: Sx = { display: "flex", flexWrap: "wrap", gap: 0.75, mt: 1 };
export const actionsSx: Sx = { display: "flex", gap: 0.25, mt: -0.25, ml: 0.25 };
export const progressSx: Sx = { display: "flex", alignItems: "center", gap: 0.75, flexWrap: "wrap" };
export const progressLabelSx: Sx = { fontSize: FS.sm, color: "text.secondary", letterSpacing: ".01em" };
export const skelSx: Sx = { display: "flex", gap: 0.625, pt: 1.5, pr: 1, pb: 1.5, pl: 1.75 };
export const skelDotSx = (i: number): Sx => ({
  width: 6,
  height: "calc(var(--sp-1) - 2px)",
  borderRadius: "50%",
  bgcolor: "text.secondary",
  animation: `${bounce} .8s infinite`,
  animationDelay: `${i * 0.12}s`,
});

export const citesSx: Sx = { display: "flex", flexWrap: "wrap", gap: 0.75, mt: 0.25 };
export const citeSx = (cube: boolean): Sx =>
  sx((t) => ({
    display: "inline-flex",
    alignItems: "center",
    gap: 0.625,
    fontSize: FS.xs,
    fontWeight: 500,
    border: `1px solid ${line(t)}`,
    borderRadius: "var(--r-pill)",
    py: 0.5,
    px: 1.375,
    color: "text.secondary",
    bgcolor: "background.paper",
    transition: "all .15s ease",
    "& b": { color: "text.primary", fontWeight: 600 },
    ...(cube
      ? { borderColor: "primary.light", bgcolor: soft(t), color: "primary.dark", boxShadow: `0 0 12px ${soft(t)}`, "& b": { color: "primary.dark", fontWeight: 600 } }
      : null),
  }));

export const footSx: Sx = { display: "flex", alignItems: "center", flexWrap: "wrap", gap: 1.25, fontSize: FS.xs, color: "text.secondary", mt: 0.5 };
export const modeSx = (degraded: boolean): Sx => ({ display: "inline-flex", alignItems: "center", gap: 0.5, ...(degraded ? { color: "error.main" } : null) });

export const bannerSx = (kind: "err" | "warn"): Sx =>
  sx((t) => {
    const c = kind === "err" ? t.vars.palette.error : t.vars.palette.warning;
    return {
      mx: 2,
      mb: 1.5,
      py: 1.25,
      px: 1.5,
      borderRadius: "var(--r-lg)",
      fontSize: FS.sm,
      lineHeight: 1.5,
      display: "flex",
      alignItems: "center",
      gap: 1,
      animation: `${slideUp} .3s ease`,
      bgcolor: varAlpha(c.mainChannel, 0.16),
      border: `1px solid ${varAlpha(c.mainChannel, 0.36)}`,
      color: c.dark,
      ...t.applyStyles("dark", { color: c.light }),
    };
  });

// ---- agent steps -----------------------------------------------------------------------------
export const stepsSx: Sx = { mt: 0.25, mb: 1, fontSize: FS.smh, lineHeight: 1.45, color: "text.secondary", maxWidth: 1 };
export const stepsHeadSx = (live: boolean): Sx =>
  sx((t) => ({
    display: "inline-flex",
    alignItems: "center",
    gap: 0.625,
    color: "text.secondary",
    font: "inherit",
    fontSize: FS.smh,
    fontWeight: 500,
    py: 0.375,
    px: 0,
    "&:hover": { color: "text.primary" },
    ...(live
      ? {
          cursor: "default",
          background: `linear-gradient(90deg, ${t.vars.palette.text.secondary} 0%, ${t.vars.palette.text.primary} 50%, ${t.vars.palette.text.secondary} 100%)`,
          backgroundSize: "200% 100%",
          WebkitBackgroundClip: "text",
          backgroundClip: "text",
          color: "transparent",
          "&:hover": { color: "transparent" },
          animation: `${shimmer} 1.8s linear infinite`,
          [REDUCED]: { animation: "none" },
        }
      : null),
  }));
export const stepsChevSx: Sx = { fontSize: FS.sm, opacity: 0.8 };
export const stepsListSx: Sx = { listStyle: "none", mt: 0.25, mb: 0, mx: 0, p: 0, display: "flex", flexDirection: "column", gap: "1px" };
export const stepSx = (kind: "more" | "now" | "done"): Sx => ({
  display: "flex",
  alignItems: "flex-start",
  gap: 0.875,
  py: "1px",
  minWidth: 0,
  ...(kind === "more" ? { pl: 2.375, fontSize: FS.xsh, opacity: 0.7 } : null),
  ...(kind === "now" ? { color: "text.primary" } : null),
});
export const stepIconSx = (now: boolean): Sx =>
  sx((t) => ({
    flex: "none",
    width: 12,
    height: "var(--sp-1h)",
    mt: 0.375,
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    color: "primary.main",
    ...(now
      ? {
          border: `1.5px solid ${varAlpha(t.vars.palette.primary.mainChannel, 0.35)}`,
          borderTopColor: "primary.main",
          borderRadius: "50%",
          animation: `${spin} .8s linear infinite`,
          [REDUCED]: { animation: "none" },
        }
      : null),
  }));
export const stepTextSx: Sx = { minWidth: 0, overflowWrap: "anywhere" };

// ---- suggestions, attachments tray, composer -------------------------------------------------
export const suggestBarSx: Sx = sx((t) => ({
  display: "flex",
  justifyContent: "flex-start",
  pt: 0,
  px: 2,
  pb: 1,
  "& .MuiButtonBase-root": {
    display: "inline-flex",
    alignItems: "center",
    gap: 0.75,
    border: `1px solid ${line(t)}`,
    bgcolor: "background.neutral",
    color: "text.primary",
    borderRadius: "var(--r2)",
    py: 0.875,
    px: 1.25,
    fontSize: FS.sm,
    fontWeight: 600,
    transition: "all .15s ease",
    '&:hover, &[aria-expanded="true"]': { borderColor: "primary.light", bgcolor: soft(t), color: "primary.dark" },
  },
}));

export const startersSx: Sx = sx((t) => ({
  display: "flex",
  flexWrap: "wrap",
  gap: 1,
  pt: 0,
  px: 2,
  pb: 1.5,
  maxHeight: 164,
  overflowY: "auto",
  overscrollBehavior: "contain",
  [phone(t)]: { flexWrap: "nowrap", overflowX: "auto", pt: 0.5, px: 1.5, pb: 1.25, scrollbarWidth: "none" },
  "& .MuiButtonBase-root": {
    display: "flex",
    alignItems: "center",
    gap: 0.75,
    border: `1px solid ${line(t)}`,
    bgcolor: "background.neutral",
    color: "text.primary",
    borderRadius: "var(--r-pill)",
    py: 1,
    px: 1.75,
    fontSize: FS.sm,
    textAlign: "left",
    transition: "all .15s ease",
    "&::before": { content: '"✨"', fontSize: FS.md },
    "&:hover": { borderColor: "primary.light", bgcolor: soft(t) },
    "&.Mui-disabled": { opacity: 0.5 },
    [phone(t)]: { flex: "none", maxWidth: "78vw", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" },
  },
}));

export const filesTraySx: Sx = sx((t) => ({ display: "flex", flexWrap: "wrap", gap: 1, pt: 1.25, px: 2, pb: 0.5, borderTop: `1px solid ${line(t)}` }));

export const formSx: Sx = sx((t) => ({
  display: "flex",
  gap: 0.75,
  alignItems: "flex-end",
  py: 1,
  px: 1.25,
  borderTop: `1px solid ${line(t)}`,
  bgcolor: "background.neutral",
  [up(t)]: { gap: 1, p: 1.5 },
}));

export const composerSx: Sx = sx((t) => ({
  flex: 1,
  minWidth: 0,
  p: 0,
  font: "inherit",
  "& .MuiInputBase-input": {
    resize: "none",
    border: `1px solid ${line(t)}`,
    borderRadius: "var(--r-lg)",
    py: 1.125,
    px: 1.375,
    fontSize: FS.input,
    lineHeight: 1.4,
    bgcolor: "background.paper",
    color: "text.primary",
    maxHeight: 120,
    minWidth: 0,
    transition: "border-color .15s ease",
    "&:focus": { borderColor: "primary.main", boxShadow: `0 0 0 3px ${varAlpha(t.vars.palette.primary.mainChannel, 0.3)}` },
    "&::placeholder": { color: "text.secondary", opacity: 1, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" },
    [up(t)]: { py: 1.25, px: 1.5, fontSize: FS.md },
  },
}));

export const toolSx = (on: boolean): Sx =>
  sx((t) => ({
    flex: "none",
    width: 38,
    height: "calc(var(--sp-5) - 2px)",
    borderRadius: "var(--r2)",
    border: `1px solid ${line(t)}`,
    color: "text.secondary",
    "&:hover": { color: "text.primary", bgcolor: greyA(t, 0.14) },
    ...(on
      ? {
          color: "common.white",
          bgcolor: "error.main",
          borderColor: "error.main",
          "&:hover": { color: "common.white", bgcolor: "error.main" },
          animation: `${pulse} 1.2s ease-in-out infinite`,
        }
      : null),
    [phone(t)]: { width: 44, height: "var(--tap-min)" },
  }));

export const sendSx = (stop: boolean): Sx =>
  sx((t) => ({
    width: 40,
    height: "var(--sp-5)",
    flex: "none",
    borderRadius: "var(--r-lg)",
    bgcolor: "primary.main",
    color: "primary.contrastText",
    transition: `all .15s ${EASE}`,
    boxShadow: t.vars.customShadows.z1,
    "&:hover": { bgcolor: "primary.dark", transform: "translateY(-2px)", boxShadow: t.vars.customShadows.z8 },
    "&:active": { transform: "translateY(0)" },
    "&.Mui-disabled": { opacity: 0.5 },
    ...(stop
      ? {
          bgcolor: "background.paper",
          color: "text.primary",
          border: `1.5px solid ${t.vars.palette.primary.main}`,
          "&:hover": { bgcolor: soft(t), transform: "none" },
        }
      : null),
    [phone(t)]: { width: 44, height: "var(--tap-min)" },
  }));

/** Floating launcher, only when the shell offers no header slot. */
export const launcherSx: Sx = sx((t) => ({
  width: 56,
  height: "var(--input-h)",
  borderRadius: "50%",
  border: `2px solid ${t.vars.palette.primary.main}`,
  bgcolor: "primary.main",
  color: "primary.contrastText",
  boxShadow: t.vars.customShadows.z8,
  transition: `transform .2s ${EASE}, box-shadow .2s ease`,
  touchAction: "none",
  userSelect: "none",
  WebkitUserSelect: "none",
  "&:hover": { transform: "translateY(-4px)", boxShadow: t.vars.customShadows.z12 },
  "&:active": { transform: "translateY(-2px)" },
  [REDUCED]: { transition: "none" },
  [phone(t)]: { width: 44, height: "var(--tap-min)", "& svg": { width: 22, height: "calc(var(--sp-3) - 2px)" } },
}));

// ---- attachments + lightbox ------------------------------------------------------------------
export const thumbSx: Sx = { position: "relative", display: "inline-flex" };
export const thumbOpenSx = (image: boolean, inUserMessage: boolean): Sx =>
  sx((t) => ({
    display: "inline-flex",
    alignItems: "center",
    gap: 0.75,
    height: "var(--input-h)",
    maxWidth: 200,
    px: image ? 0 : 1.25,
    width: image ? 56 : undefined,
    border: `1px solid ${line(t)}`,
    borderRadius: "var(--r2)",
    bgcolor: inUserMessage ? "background.paper" : "background.neutral",
    color: "text.primary",
    fontSize: FS.sm,
    cursor: "zoom-in",
    overflow: "hidden",
    "& img": { width: 1, height: "100%", objectFit: "cover", display: "block" },
  }));
export const thumbNameSx: Sx = { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
export const thumbRemoveSx: Sx = sx((t) => ({
  position: "absolute",
  top: -6,
  right: -6,
  width: 18,
  height: "calc(var(--sp-2) + 2px)",
  borderRadius: "50%",
  border: `1px solid ${line(t)}`,
  bgcolor: "background.paper",
  color: "text.primary",
  p: 0,
}));

export const lightboxPaperSx: Sx = sx((t) => ({
  bgcolor: varAlpha(t.vars.palette.grey["900Channel"], 0.92),
  display: "flex",
  flexDirection: "column",
}));
const roundWhite = (t: Theme) => ({
  border: 0,
  bgcolor: whiteA(t, 0.12),
  color: "common.white",
  borderRadius: "50%",
  width: 40,
  height: "var(--sp-5)",
});
export const lightboxTopSx: Sx = sx((t) => ({
  display: "flex",
  alignItems: "center",
  gap: 1.5,
  py: 1.5,
  px: 2,
  color: "common.white",
  fontSize: FS.md,
  "& .MuiButtonBase-root": roundWhite(t),
}));
export const lightboxNameSx: Sx = { flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" };
export const lightboxCountSx: Sx = { opacity: 0.7 };
export const lightboxBodySx: Sx = sx((t) => ({
  flex: 1,
  minHeight: 0,
  display: "flex",
  alignItems: "center",
  justifyContent: "center",
  pt: 1,
  px: 8,
  pb: 3,
  [phone(t)]: { px: 1 },
  "& img": { maxWidth: 1, maxHeight: 1, objectFit: "contain", borderRadius: "var(--r-md)" },
  // The iframe shows an HTML DOCUMENT the answer attached, not an app surface: documents assume a
  // white canvas (black default text), so it keeps the page white in both modes.
  "& iframe": { width: "min(calc(var(--sp-5) * 25), 100%)", height: "100%", border: 0, borderRadius: "var(--r-md)", bgcolor: "var(--palette-common-white)" },
}));
export const lightboxCardSx: Sx = {
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  gap: 1.5,
  color: "common.white",
  "& a": { color: "primary.main" },
};
export const lightboxNavSx = (side: "prev" | "next"): Sx =>
  sx((t) => ({
    ...roundWhite(t),
    position: "absolute",
    top: "50%",
    transform: "translateY(-50%)",
    ...(side === "prev" ? { left: 12 } : { right: 12 }),
    [phone(t)]: { top: "auto", bottom: 20, transform: "none" },
  }));

// ---- copy buttons ----------------------------------------------------------------------------
export const copyIconSx = (inCode: boolean): Sx =>
  sx((t) => ({
    width: inCode ? 24 : 26,
    height: inCode ? "var(--sp-3)" : "calc(var(--sp-3) + 2px)",
    p: 0,
    borderRadius: "var(--r-sm)",
    color: inCode ? t.vars.palette.grey[500] : "text.secondary",
    opacity: 0.75,
    transition: "opacity .15s, background .15s",
    "&:hover": { opacity: 1, bgcolor: greyA(t, 0.16), color: inCode ? t.vars.palette.grey[200] : "text.primary" },
    ...(inCode ? t.applyStyles("light", { color: t.vars.palette.text.secondary, "&:hover": { color: t.vars.palette.text.primary } }) : null),
  }));

// ---- markdown answer -------------------------------------------------------------------------
export const markdownSx: Sx = sx((t) => ({
  whiteSpace: "normal",
  fontSize: FS.mdh,
  lineHeight: 1.65,
  maxWidth: 1,
  minWidth: 0,
  "& > :first-child": { mt: 0 },
  "& > :last-child": { mb: 0 },
  "& p": { m: "0 0 .7em", overflowWrap: "anywhere" },
  "& ul, & ol": { m: "0 0 .7em", pl: "1.3em" },
  "& ul": { listStyle: "disc" },
  "& ol": { listStyle: "decimal" },
  "& li": { m: ".25em 0", overflowWrap: "anywhere" },
  "& li > p": { m: 0 },
  "& li::marker": { color: "text.secondary" },
  "& h1, & h2, & h3, & h4": { fontWeight: 700, lineHeight: 1.3, m: "1em 0 .45em" },
  "& h1": { fontSize: "125%" },
  "& h2": { fontSize: "112%" },
  "& h3, & h4": { fontSize: "100%" },
  "& strong": { fontWeight: 700 },
  "& a": { color: "primary.main", textDecoration: "underline", textUnderlineOffset: "2px", overflowWrap: "anywhere" },
  "& blockquote": { m: "0 0 .7em", p: ".1em .9em", borderLeft: `3px solid ${t.vars.palette.primary.main}`, opacity: 0.9 },
  "& hr": { border: 0, borderTop: `1px solid ${line(t)}`, m: "1em 0" },
  "& :not(pre) > code": { fontFamily: MONO, fontSize: "86%", p: ".12em .38em", borderRadius: "var(--r-sm)", bgcolor: greyA(t, 0.18) },
  "& img, & svg": { maxWidth: 1, height: "auto" },
  "& table": { borderCollapse: "collapse", width: 1, fontSize: "92%" },
  "& th, & td": { p: ".45em .75em", textAlign: "left", whiteSpace: "nowrap", borderBottom: `1px solid ${line(t)}` },
  "& tr:last-child td": { borderBottom: 0 },
  "& th": { fontWeight: 700, bgcolor: greyA(t, 0.1) },
  "& td:not(:first-of-type), & th:not(:first-of-type)": { fontVariantNumeric: "tabular-nums" },
}));

export const markdownFallbackSx: Sx = [markdownSx, { "& p": { whiteSpace: "pre-wrap" } }] as unknown as Sx;

export const markdownTableSx: Sx = sx((t) => ({
  overflowX: "auto",
  m: ".3em 0 .9em",
  border: `1px solid ${line(t)}`,
  borderRadius: "var(--r2)",
  maxWidth: 1,
  minWidth: 0,
}));

const hljsDark = (t: Theme) => ({
  "& .hljs-comment, & .hljs-quote": { color: t.vars.palette.grey[500], fontStyle: "italic" },
  "& .hljs-keyword, & .hljs-selector-tag, & .hljs-literal, & .hljs-type": { color: t.vars.palette.error.light },
  "& .hljs-string, & .hljs-regexp, & .hljs-addition": { color: t.vars.palette.info.lighter },
  "& .hljs-number, & .hljs-symbol, & .hljs-bullet": { color: t.vars.palette.info.light },
  "& .hljs-title, & .hljs-section, & .hljs-title.function_": { color: t.vars.palette.secondary.light },
  "& .hljs-attr, & .hljs-attribute, & .hljs-variable, & .hljs-template-variable, & .hljs-property": { color: t.vars.palette.info.light },
  "& .hljs-built_in, & .hljs-name, & .hljs-tag": { color: t.vars.palette.success.light },
  "& .hljs-meta": { color: t.vars.palette.warning.light },
  "& .hljs-deletion": { color: t.vars.palette.error.lighter },
});
const hljsLight = (t: Theme) => ({
  "& .hljs-comment, & .hljs-quote": { color: t.vars.palette.text.secondary },
  "& .hljs-keyword, & .hljs-selector-tag, & .hljs-literal, & .hljs-type": { color: t.vars.palette.error.main },
  "& .hljs-string, & .hljs-regexp, & .hljs-addition": { color: t.vars.palette.success.dark },
  "& .hljs-number, & .hljs-symbol, & .hljs-bullet, & .hljs-attr, & .hljs-attribute, & .hljs-variable, & .hljs-template-variable, & .hljs-property": {
    color: t.vars.palette.info.main,
  },
  "& .hljs-title, & .hljs-section, & .hljs-title.function_": { color: t.vars.palette.secondary.main },
  "& .hljs-built_in, & .hljs-name, & .hljs-tag": { color: t.vars.palette.primary.dark },
  "& .hljs-meta": { color: t.vars.palette.warning.main },
  "& .hljs-deletion": { color: t.vars.palette.error.main },
});

/** Code block: the GitHub-dark console in dark mode, the light panel tokens in light mode. */
export const codeBlockSx: Sx = sx((t) => ({
  m: ".3em 0 .9em",
  border: `1px solid ${line(t)}`,
  borderRadius: "var(--r2)",
  overflow: "hidden",
  bgcolor: t.vars.palette.grey[900],
  maxWidth: 1,
  minWidth: 0,
  ...hljsDark(t),
  ...t.applyStyles("light", { bgcolor: t.vars.palette.background.neutral, ...hljsLight(t) }),
}));
export const codeHeadSx: Sx = sx((t) => ({
  display: "flex",
  justifyContent: "space-between",
  alignItems: "center",
  pt: ".35em",
  pb: ".35em",
  pl: ".8em",
  pr: ".5em",
  fontSize: FS.xsh,
  color: t.vars.palette.grey[500],
  bgcolor: t.vars.palette.grey[800],
  borderBottom: `1px solid ${t.vars.palette.grey[700]}`,
  ...t.applyStyles("light", {
    bgcolor: t.vars.palette.background.default,
    color: t.vars.palette.text.secondary,
    borderBottomColor: t.vars.palette.divider,
  }),
}));
export const codeActionsSx: Sx = { display: "flex", gap: 0.75 };
export const codePreSx: Sx = sx((t) => ({
  m: 0,
  p: ".8em .9em",
  overflowX: "auto",
  color: t.vars.palette.grey[200],
  fontFamily: MONO,
  fontSize: FS.smh,
  lineHeight: 1.55,
  whiteSpace: "pre",
  "& code": { background: "none", p: 0, fontSize: "inherit" },
  ...t.applyStyles("light", { color: t.vars.palette.text.primary }),
}));
export const codePreviewToggleSx: Sx = sx((t) => ({
  font: "inherit",
  fontSize: FS.xsh,
  p: ".25em .6em",
  borderRadius: "var(--r-sm)",
  border: `1px solid ${greyA(t, 0.35)}`,
  color: "inherit",
  "&:hover": { bgcolor: greyA(t, 0.18) },
}));
// Same as the lightbox iframe: an attached HTML document renders on its own white canvas.
export const htmlPreviewSx: Sx = { display: "block", width: 1, height: "calc(var(--sp-4) * 10)", border: 0, bgcolor: "var(--palette-common-white)" };

// ---- chart card ------------------------------------------------------------------------------
export const chartSx: Sx = sx((t) => ({
  mt: 1,
  mb: 0.25,
  mx: 0,
  py: 1.25,
  px: 1.5,
  bgcolor: "background.neutral",
  border: `1px solid ${line(t)}`,
  borderRadius: "calc(var(--r-lg) + 2px)",
  maxWidth: 1,
  overflow: "hidden",
  boxSizing: "border-box",
  width: 1,
}));
export const chartTitleSx: Sx = { fontSize: FS.sm, fontWeight: 600, color: "text.secondary", mt: 0, mx: 0, mb: 1, letterSpacing: ".01em", whiteSpace: "normal" };

// ---- live tag watch card ---------------------------------------------------------------------
export type Tone = "ok" | "teal" | "warn" | "dng" | "pur" | "mut";
const toneColour = (t: Theme, tone: string) =>
  ({ ok: t.vars.palette.success, teal: t.vars.palette.info, warn: t.vars.palette.warning, dng: t.vars.palette.error, pur: t.vars.palette.secondary })[tone];

export const watchSx = (live: boolean): Sx =>
  sx((t) => ({
    border: `1px solid ${live ? varAlpha(t.vars.palette.primary.mainChannel, 0.45) : line(t)}`,
    borderRadius: "var(--r-lg)",
    bgcolor: "background.paper",
    mt: 0.5,
    mb: 1.25,
    mx: 0,
    overflow: "hidden",
    maxWidth: 1,
  }));
export const watchHeadSx: Sx = sx((t) => ({
  display: "flex",
  alignItems: "center",
  gap: 1.25,
  flexWrap: "wrap",
  py: 1.25,
  px: 1.5,
  borderBottom: `1px solid ${line(t)}`,
  bgcolor: "background.neutral",
  [phone(t)]: { px: 1.25 },
}));
export const watchDotSx = (live: boolean): Sx =>
  sx((t) => ({
    width: 8,
    height: "var(--sp-1)",
    borderRadius: "50%",
    bgcolor: "text.secondary",
    flex: "none",
    ...(live
      ? {
          bgcolor: "primary.main",
          boxShadow: `0 0 0 0 ${varAlpha(t.vars.palette.primary.mainChannel, 0.6)}`,
          animation: `${watchPulse} 1.6s ease-out infinite`,
          [REDUCED]: { animation: "none" },
        }
      : null),
  }));
export const watchTitleSx: Sx = {
  display: "flex",
  flexDirection: "column",
  minWidth: 0,
  flex: "1 1 160px",
  "& strong": { fontSize: FS.md, color: "text.primary" },
};
export const watchSubSx: Sx = { fontSize: FS.xsh, color: "text.secondary", overflowWrap: "anywhere" };
export const watchCountSx: Sx = { fontSize: FS.sm, color: "text.secondary", fontVariantNumeric: "tabular-nums" };
export const watchStopSx: Sx = sx((t) => ({
  border: `1.5px solid ${t.vars.palette.primary.main}`,
  bgcolor: "background.paper",
  color: "text.primary",
  borderRadius: "var(--r-pill)",
  py: 0.625,
  px: 1.5,
  fontSize: FS.sm,
  fontWeight: 600,
  minHeight: "var(--sp-4)",
  "&:hover": { bgcolor: soft(t) },
}));
export const watchNoteSx: Sx = { my: 1, mx: 1.5, fontSize: FS.sm, color: "text.secondary" };
export const watchScrollSx: Sx = { overflowX: "auto", WebkitOverflowScrolling: "touch", maxHeight: 320, overflowY: "auto" };
export const watchTableSx: Sx = sx((t) => ({
  borderCollapse: "collapse",
  width: 1,
  fontSize: FS.sm,
  color: "text.primary",
  "& th": {
    position: "sticky",
    top: 0,
    bgcolor: "background.paper",
    fontWeight: 600,
    color: "text.secondary",
    py: 0.75,
    px: 1.25,
    fontSize: FS.sm,
    borderBottom: `1px solid ${line(t)}`,
    whiteSpace: "nowrap",
  },
  "& td": { py: 0.75, px: 1.25, fontSize: FS.sm, color: "text.primary", borderBottom: `1px solid ${line(t)}`, verticalAlign: "top", whiteSpace: "nowrap" },
  "& .MuiTableCell-alignRight": { fontVariantNumeric: "tabular-nums" },
  [phone(t)]: { "& td, & th": { px: 1 } },
}));
export const watchTagSx: Sx = { fontWeight: 600 };
export const watchDimSx: Sx = { display: "block", fontSize: FS.xs, color: "text.secondary" };
export const watchPillSx = (tone: string): Sx =>
  sx((t) => {
    const c = toneColour(t, tone);
    return {
      display: "inline-block",
      borderRadius: "var(--r-pill)",
      py: "1px",
      px: 1,
      fontSize: FS.xs,
      fontWeight: 600,
      bgcolor: c ? varAlpha(c.mainChannel, 0.16) : varAlpha(t.vars.palette.text.secondaryChannel, 0.14),
      color: "text.primary",
    };
  });
export const watchPctSx = (tone: string): Sx => ({
  fontWeight: 600,
  color: tone === "dng" ? "error.main" : tone === "warn" ? "warning.main" : "text.secondary",
});
export const watchFeedSx: Sx = {
  listStyle: "none",
  m: 0,
  py: 1,
  px: 1.5,
  display: "flex",
  flexDirection: "column",
  gap: 0.5,
  maxHeight: 160,
  overflowY: "auto",
  fontSize: FS.sm,
  color: "text.primary",
};
export const watchFeedItemSx = (tone: string): Sx =>
  sx((t) => ({
    display: "flex",
    gap: 1,
    alignItems: "baseline",
    borderLeft: `3px solid ${tone === "ok" ? t.vars.palette.primary.main : tone === "warn" ? t.vars.palette.warning.main : tone === "dng" ? t.vars.palette.error.main : line(t)}`,
    pl: 1,
  }));
export const watchAtSx: Sx = { flex: "none", minWidth: 34, color: "text.secondary", fontVariantNumeric: "tabular-nums" };

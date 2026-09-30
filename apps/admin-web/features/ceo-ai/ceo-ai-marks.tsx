// Ask Mesha brand marks: the Twemoji goat (header launcher) and the Mesha मे logo (chat header and
// answer avatar). Colours are theme palette tokens; the goat's bob is an MUI keyframe that reduced
// motion turns off. Everything else in the assistant is the template chat section
// (components/app/sections/chat) and MUI parts.
//
// Goat mascot: Twitter Twemoji goat (U+1F410), © Twitter, licensed CC-BY 4.0
// (https://github.com/twitter/twemoji). Attribution retained per the CC-BY 4.0 license; see
// docs/ceo-ai/access-policy.md / mascot note.

import type { ReactElement } from "react";
import Box from "@mui/material/Box";
import { keyframes } from "@mui/material/styles";

const goatBob = keyframes`0%,100%{transform:translateY(0)}50%{transform:translateY(-4%)}`;

/** The goat mark; `size` is its box (the floating launcher 28, the header dock a spacing token). */
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
      <Box
        component="g"
        sx={{
          transformOrigin: "50% 83%",
          "@media (prefers-reduced-motion: no-preference)": { animation: `${goatBob} 2.6s ease-in-out infinite` },
        }}
      >
        <path fill="var(--palette-warning-main)" d="M7.44 7.503c-1-4 3.687-6 8-4 .907.421.948 1.316 0 1-3-1-6 1-4 4 1.109 1.664-3.233 2.068-4-1z" />
        <path fill="var(--palette-warning-light)" d="M6.136 5.785c-1-4 3.687-6 8-4 .907.421.949 1.316 0 1-3-1-6 1-4 4 1.11 1.664-3.233 2.067-4-1z" />
        <path fill="var(--palette-grey-200)" d="M5 14.785c0 4-2 4.827-2 4 0-2-1 0-1-1v-3c0-1.657.671-3 1.5-3s1.5 1.343 1.5 3z" />
        <path fill="var(--palette-grey-300)" d="M35.159 10.49c-.68-1.643-2.313-2.705-4.159-2.705-.553 0-1 .448-1 1s.447 1 1 1c1.034 0 1.941.577 2.312 1.471.341.824.168 1.758-.455 2.647-.984-1.506-2.602-2.618-4.856-2.618-2.391 0-7.279.714-10.828 1.289-.052-.094-.105-.188-.172-.289-2-3-4-8.157-7-8.157-4 0-10 4.986-10 9.157 0 2.544 5.738 2.929 7.486 2.988.697 1.43 1.414 2.934 2.232 4.33.066.205.155.429.282.683 3 6 3.119 14.5 4.5 14.5s2.5-4.857 2.5-9c0-.151-.004-.299-.007-.447 3.126.649 6.607.322 9.677-.61 1.448 5.045 1.77 10.058 2.83 10.058 1.342 0 2.433-8.818 2.494-13.12C33.316 21.226 34 19.51 34 17.785c0-.605-.086-1.23-.248-1.843 1.614-1.644 2.143-3.676 1.407-5.452z" />
        <circle fill="var(--palette-grey-800)" cx="7" cy="9.285" r="1" />
      </Box>
    </Box>
  );
}

/** The Mesha मे mark (same as the shell logo tile): fills the template Avatar it sits in. */
export function MeshaLogo({ size = 40 }: { size?: number }): ReactElement {
  return (
    <Box
      component="span"
      aria-label="Mesha"
      role="img"
      sx={{
        width: 1,
        height: 1,
        display: "grid",
        placeItems: "center",
        bgcolor: "primary.main",
        color: "primary.contrastText",
        fontWeight: 800,
        fontSize: Math.round(size * 0.47),
        lineHeight: 1,
      }}
    >
      मे
    </Box>
  );
}

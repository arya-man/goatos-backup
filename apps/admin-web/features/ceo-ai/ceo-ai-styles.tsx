// Self-contained styles for the leadership assistant surface. Kept inside the
// feature (not app/mesha-theme.css) so the assistant owns its own chrome, but
// every colour references the shared mesha theme tokens (--brand, --panel,
// --ink, --line, --danger, ...) so it stays theme-consistent and never
// introduces the banned old-admin palette. Class prefix `mzai-` avoids clashing
// with the legacy `ceo-ai-*` classes.
//
// Goat mascot: Twitter Twemoji goat (U+1F410), © Twitter, licensed CC-BY 4.0
// (https://github.com/twitter/twemoji). A real, recognizable side-profile goat
// used for the launcher, header, and message avatars, and (with a trot bob) for
// the in-progress send button. Attribution retained per the CC-BY 4.0 license;
// see docs/ceo-ai/access-policy.md / mascot note.

import type { ReactElement } from "react";
import Box from "@mui/material/Box";
import { WATCH_CSS } from "./ceo-ai-watch-css";


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

export function GoatAvatar(): ReactElement {
  return (
    <svg
      width="30"
      height="30"
      viewBox="0 0 36 36"
      xmlns="http://www.w3.org/2000/svg"
      className="mzai-goat-icon"
      role="img"
      aria-label="Mesha goat assistant"
    >
      <defs>
        <style>{`
          @media (prefers-reduced-motion: no-preference) {
            @keyframes goat-bob { 0%,100%{transform:translateY(0)} 50%{transform:translateY(-1.5px)} }
            .mzai-goat-icon .goat-bob { animation: goat-bob 2.6s ease-in-out infinite; transform-origin: 18px 30px; }
          }
        `}</style>
      </defs>
      <g className="goat-bob">
        <GoatArt />
      </g>
    </svg>
  );
}

export function GoatWalking(): ReactElement {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 36 36"
      xmlns="http://www.w3.org/2000/svg"
      className="mzai-goat-walking"
      role="img"
      aria-label="Generating answer"
    >
      <defs>
        <style>{`
          @media (prefers-reduced-motion: no-preference) {
            @keyframes gwalk-trot {
              0%,100%{transform:translateY(0) rotate(0deg)}
              25%{transform:translateY(-1px) rotate(-2deg)}
              50%{transform:translateY(0) rotate(0deg)}
              75%{transform:translateY(-1px) rotate(2deg)}
            }
            .mzai-goat-walking .goat-trot { animation: gwalk-trot 0.7s ease-in-out infinite; transform-origin: 18px 30px; }
          }
        `}</style>
      </defs>
      <g className="goat-trot">
        <GoatArt />
      </g>
    </svg>
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

export function CeoAiStyles(): ReactElement {
  return (
    <style>{`
.mzai-root{position:fixed;bottom:24px;right:24px;z-index:80;font-family:var(--f);transition:all .3s cubic-bezier(.34,.1,.64,.9);
  overscroll-behavior:contain}
.mzai-md{white-space:normal;font-size:13.5px;line-height:1.65}
.mzai-md>*:first-child{margin-top:0}.mzai-md>*:last-child{margin-bottom:0}
.mzai-md p{margin:0 0 .7em}
.mzai-md ul,.mzai-md ol{margin:0 0 .7em;padding-left:1.3em}
.mzai-md li{margin:.25em 0}.mzai-md li>p{margin:0}
.mzai-md h1,.mzai-md h2,.mzai-md h3,.mzai-md h4{font-weight:700;line-height:1.3;margin:1em 0 .45em}
.mzai-md h1{font-size:1.25em}.mzai-md h2{font-size:1.12em}.mzai-md h3,.mzai-md h4{font-size:1em}
.mzai-md strong{font-weight:700}
.mzai-md a{color:var(--brand);text-decoration:underline;text-underline-offset:2px;overflow-wrap:anywhere}
.mzai-md p,.mzai-md li{overflow-wrap:anywhere}
.mzai-md blockquote{margin:0 0 .7em;padding:.1em .9em;border-left:3px solid var(--brand);opacity:.9}
.mzai-md hr{border:0;border-top:1px solid var(--line);margin:1em 0}
.mzai-md :not(pre)>code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.86em;padding:.12em .38em;border-radius:5px;background:rgba(var(--palette-grey-500Channel) / 0.18)}
.mzai-table{overflow-x:auto;margin:.3em 0 .9em;border:1px solid var(--line);border-radius:10px}
.mzai-md table{border-collapse:collapse;width:100%;font-size:.92em}
.mzai-md th,.mzai-md td{padding:.45em .75em;text-align:left;white-space:nowrap;border-bottom:1px solid var(--line)}
.mzai-md tr:last-child td{border-bottom:0}
.mzai-md th{font-weight:700;background:rgba(var(--palette-grey-500Channel) / 0.1)}
.mzai-md td:not(:first-child),.mzai-md th:not(:first-child){font-variant-numeric:tabular-nums}
.mzai-code{margin:.3em 0 .9em;border:1px solid var(--line);border-radius:10px;overflow:hidden;background:var(--palette-grey-900)}
.mzai-code-head{display:flex;justify-content:space-between;align-items:center;padding:.35em .5em .35em .8em;font-size:11.5px;color:var(--palette-grey-500);background:var(--palette-grey-800);border-bottom:1px solid var(--palette-grey-700)}
.mzai-code-actions{display:flex;gap:6px}
.mzai-code pre{margin:0;padding:.8em .9em;overflow-x:auto;color:var(--palette-grey-200);font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12.5px;line-height:1.55;white-space:pre}
.mzai-code pre code{background:none;padding:0;font-size:inherit}
.mzai-html{display:block;width:100%;height:320px;border:0;background:var(--palette-common-white)}
.mzai-copy{font:inherit;font-size:11.5px;padding:.25em .6em;border-radius:6px;border:1px solid rgba(var(--palette-grey-500Channel) / 0.35);background:transparent;color:inherit;cursor:pointer}
.mzai-copy:hover{background:rgba(var(--palette-grey-500Channel) / 0.18)}
.mzai-copy-ic{display:inline-flex;align-items:center;justify-content:center;width:26px;height:26px;padding:0;
  border:0;border-radius:6px;background:transparent;color:var(--muted);cursor:pointer;opacity:.75;transition:opacity .15s,background .15s}
.mzai-copy-ic:hover{opacity:1;background:rgba(var(--palette-grey-500Channel) / 0.16);color:var(--ink)}
.mzai-code .mzai-copy-ic{color:var(--palette-grey-500);width:24px;height:24px}
.mzai-code .mzai-copy-ic:hover{color:var(--palette-grey-200)}
.hljs-comment,.hljs-quote{color:var(--palette-grey-500);font-style:italic}
.hljs-keyword,.hljs-selector-tag,.hljs-literal,.hljs-type{color:var(--palette-error-light)}
.hljs-string,.hljs-regexp,.hljs-addition{color:var(--palette-info-lighter)}
.hljs-number,.hljs-symbol,.hljs-bullet{color:var(--palette-info-light)}
.hljs-title,.hljs-section,.hljs-title.function_{color:var(--palette-secondary-light)}
.hljs-attr,.hljs-attribute,.hljs-variable,.hljs-template-variable,.hljs-property{color:var(--palette-info-light)}
.hljs-built_in,.hljs-name,.hljs-tag{color:var(--palette-success-light)}
.hljs-meta{color:var(--palette-warning-light)}.hljs-deletion{color:var(--palette-error-lighter)}

/* ---- containment: nothing inside a message may widen the chat column ---- */
.mzai-log{overflow-x:hidden}
.mzai-msg-wrap{min-width:0}
.mzai-msg{min-width:0}
.mzai-msg.assistant{flex:1 1 auto;max-width:calc(100% - 40px)}
.mzai-bub{min-width:0;max-width:100%;overflow-wrap:anywhere}
.mzai-msg.assistant .mzai-bub{width:100%;box-sizing:border-box}
.mzai-md,.mzai-table,.mzai-code{max-width:100%;min-width:0}
.mzai-md img,.mzai-md svg{max-width:100%;height:auto}
/* ---- window states ---- */
.mzai-view-min .mzai-body{display:none}
.mzai-view-min .mzai-panel{height:auto}
.mzai-view-min .mzai-head{cursor:pointer;border-bottom:0}
.mzai-view-max .mzai-panel{height:100%}
.mzai-view-max .mzai-msg.assistant{max-width:min(980px,calc(100% - 40px))}
/* ---- phones and narrow webviews: full-screen sheet, threads overlay ---- */
@media (max-width:620px){
  .mzai-root.mzai-open{inset:0 !important;width:auto !important;height:auto !important}
  .mzai-open .mzai-panel{height:100dvh;border-radius:0;border:0;
    padding-top:env(safe-area-inset-top);padding-bottom:env(safe-area-inset-bottom)}
  .mzai-hide-mobile{display:none}
  .mzai-root.mzai-open.mzai-view-min{inset:auto 0 0 0 !important}
  .mzai-open.mzai-view-min .mzai-panel{height:auto;padding-top:0;border-top:1px solid var(--line)}
  .mzai-body{position:relative}
  .mzai-side{position:absolute;inset:0 auto 0 0;width:min(78vw,280px);z-index:2;box-shadow:8px 0 24px rgba(var(--palette-common-blackChannel) / 0.25)}
  .mzai-msg.assistant{max-width:calc(100% - 36px)}
  .mzai-root .mzai-form textarea{font-size:16px}
}
.mzai-tool{flex:none;width:38px;height:38px;border-radius:10px;border:1px solid var(--line);background:transparent;
  color:var(--muted);display:inline-flex;align-items:center;justify-content:center;cursor:pointer}
.mzai-tool:hover{color:var(--ink);background:rgba(var(--palette-grey-500Channel) / 0.14)}
.mzai-tool.mzai-tool-on{color:var(--palette-common-white);background:var(--danger);border-color:var(--danger);animation:mzai-pulse 1.2s ease-in-out infinite}
@keyframes mzai-pulse{50%{opacity:.7}}
.mzai-files{display:flex;flex-wrap:wrap;gap:8px;padding:10px 16px 4px;border-top:1px solid var(--line)}
.mzai-thumb{position:relative;display:inline-flex}
.mzai-thumb-open{display:inline-flex;align-items:center;gap:6px;height:56px;max-width:200px;padding:0 10px;border:1px solid var(--line);
  border-radius:10px;background:var(--panel-2);color:var(--ink);font:inherit;font-size:12px;cursor:zoom-in;overflow:hidden}
.mzai-thumb.img .mzai-thumb-open{width:56px;padding:0}
.mzai-thumb.img img{width:100%;height:100%;object-fit:cover;display:block}
.mzai-thumb-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.mzai-thumb-x{position:absolute;top:-6px;right:-6px;width:18px;height:18px;border-radius:50%;border:1px solid var(--line);
  background:var(--panel);color:var(--ink);display:flex;align-items:center;justify-content:center;cursor:pointer;padding:0}
.mzai-msg-files{display:flex;flex-wrap:wrap;gap:6px;margin-top:8px}
.mzai-msg.user .mzai-thumb-open{border-color:rgba(var(--palette-common-whiteChannel) / 0.35);background:rgba(var(--palette-common-blackChannel) / 0.12);color:var(--palette-common-white)}
.MuiDialog-paper.mzai-lb{background:rgba(var(--palette-grey-900Channel) / 0.92);display:flex;flex-direction:column}
.mzai-lb-top{display:flex;align-items:center;gap:12px;padding:12px 16px;color:var(--palette-common-white);font-size:13px}
.mzai-lb-name{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.mzai-lb-count{opacity:.7}
.mzai-lb-top button,.mzai-lb-nav{border:0;background:rgba(var(--palette-common-whiteChannel) / 0.12);color:var(--palette-common-white);border-radius:50%;width:40px;height:40px;
  display:flex;align-items:center;justify-content:center;cursor:pointer}
.mzai-lb-body{flex:1;min-height:0;display:flex;align-items:center;justify-content:center;padding:8px 64px 24px}
.mzai-lb-body img{max-width:100%;max-height:100%;object-fit:contain;border-radius:8px}
.mzai-lb-body iframe{width:min(1000px,100%);height:100%;border:0;border-radius:8px;background:var(--palette-common-white)}
.mzai-lb-card{display:flex;flex-direction:column;align-items:center;gap:12px;color:var(--palette-common-white)}
.mzai-lb-card a{color:var(--brand)}
.mzai-lb-nav{position:absolute;top:50%;transform:translateY(-50%)}
.mzai-lb-nav.prev{left:12px}.mzai-lb-nav.next{right:12px}
@media (max-width:620px){.mzai-lb-body{padding:8px 8px 24px}.mzai-lb-nav{top:auto;bottom:20px;transform:none}}
/* spacing between suggestions, attachments and composer */
.mzai-suggestbar{padding:8px 16px}
.mzai-starters{padding:4px 16px 12px}
/* maximized panel sits above the app chrome so its header stays visible */
.mzai-root.mzai-view-max{z-index:1000}
.mzai-root.mzai-open{z-index:1000}
.mzai-msg.user .mzai-bub{white-space:pre-wrap}
.mzai-actions{display:flex;gap:2px;margin:-2px 0 0 2px}
.mzai-panel{position:relative}
.mzai-drop{position:absolute;inset:8px;z-index:5;display:flex;align-items:center;justify-content:center;border:2px dashed var(--brand);
  border-radius:16px;background:rgba(var(--palette-grey-900Channel) / 0.82);color:var(--palette-common-white);font-size:15px;font-weight:600;pointer-events:none}
.mzai-bubble{width:56px;height:56px;border-radius:50%;border:2px solid var(--brand);
  background:var(--brand);color:var(--on-brand);display:flex;align-items:center;justify-content:center;
  cursor:pointer;box-shadow:0 4px 12px rgba(var(--palette-common-blackChannel) / 0.15);transition:transform .2s cubic-bezier(.34,.1,.64,.9),box-shadow .2s ease}
.mzai-bubble:hover{transform:translateY(-4px);box-shadow:0 8px 20px rgba(var(--palette-common-blackChannel) / 0.2)}
.mzai-bubble:active{transform:translateY(-2px)}
.mzai-goat-icon{width:28px;height:28px}
@media (prefers-reduced-motion: reduce) {
  .mzai-bubble{transition:none}
  .goat-bubble{animation:none !important}
  .goat-eye-left,.goat-eye-right,.goat-ear{animation:none !important}
}
.mzai-panel{display:flex;flex-direction:column;height:100%;background:var(--panel);
  border:1px solid var(--line);border-radius:20px;overflow:hidden;
  box-shadow:0 20px 60px rgba(var(--palette-common-blackChannel) / 0.2),0 0 1px rgba(var(--palette-common-blackChannel) / 0.1);
  animation:mzai-panel-open .3s cubic-bezier(.34,.1,.64,.9);overscroll-behavior:contain}
@keyframes mzai-panel-open{from{opacity:0;transform:scale(.8) translateY(8px)}to{opacity:1;transform:scale(1) translateY(0)}}
.mzai-head{display:flex;align-items:center;gap:12px;padding:14px 16px;border-bottom:1px solid var(--line);
  background:linear-gradient(135deg,var(--panel) 0%,var(--panel-2) 100%)}
/* Neutral wrapper: MeshaLogo renders its own brand disc (matching the sidebar
   brand logo), so the mark container must not add a second disc/ring. */
.mzai-mark{display:flex;align-items:center;justify-content:center;flex:none}
.mzai-mark .mzai-goat-icon{width:32px;height:32px}
.mzai-htext{display:flex;flex-direction:column;min-width:0;flex:1}
.mzai-htext b{font-size:14px;font-weight:600;color:var(--ink);line-height:1.2}
.mzai-htext small{font-size:11.5px;color:var(--muted);margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.mzai-hbtns{display:flex;align-items:center;gap:6px}
.mzai-icon{width:36px;height:36px;border-radius:10px;border:1px solid var(--line);
  background:var(--bg);color:var(--ink);display:flex;align-items:center;justify-content:center;
  cursor:pointer;transition:all .15s ease}
.mzai-icon:hover{background:var(--sidebar-2);border-color:var(--brand-l)}
.mzai-icon:active{transform:scale(.95)}
.mzai-icon[aria-pressed="true"]{background:var(--brand-soft);border-color:var(--brand);color:var(--brand-d)}
.mzai-body{display:flex;flex:1;min-height:0}
.mzai-side{width:200px;flex:none;border-right:1px solid var(--line);background:var(--panel-2);
  display:flex;flex-direction:column;min-height:0}
.mzai-side.mzai-hide{display:none}
.mzai-side-head{display:flex;align-items:center;justify-content:space-between;padding:12px 12px 10px}
.mzai-side-head span{font-size:10px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:var(--muted)}
.mzai-newbtn{display:flex;align-items:center;gap:5px;border:1px solid var(--brand);background:var(--brand);
  color:var(--on-brand);border-radius:8px;padding:6px 9px;font:inherit;font-size:11px;font-weight:600;cursor:pointer;
  transition:all .15s ease;box-shadow:0 2px 6px rgba(var(--palette-common-blackChannel) / 0.08)}
.mzai-newbtn:hover{transform:translateY(-1px);box-shadow:0 4px 12px rgba(var(--palette-common-blackChannel) / 0.12)}
.mzai-threads{flex:1;overflow-y:auto;padding:6px 8px 10px;overscroll-behavior:contain}
.mzai-thread{display:flex;align-items:center;gap:6px;border-radius:10px;padding:8px 8px;cursor:pointer;
  transition:all .12s ease}
.mzai-thread:hover{background:var(--sidebar-2)}
.mzai-thread.mzai-on{background:linear-gradient(135deg,var(--brand-soft),var(--brand-soft));
  border-left:3px solid var(--brand)}
.mzai-thread .mzai-tt{flex:1;min-width:0;font-size:12px;color:var(--ink);overflow:hidden;
  text-overflow:ellipsis;white-space:nowrap}
.mzai-thread input{flex:1;min-width:0;font:inherit;font-size:12px;border:1px solid var(--brand);
  border-radius:8px;padding:4px 8px;background:var(--panel);color:var(--ink);outline:0}
.mzai-thread-act{opacity:0;width:24px;height:24px;border:0;background:transparent;color:var(--muted);
  display:flex;align-items:center;justify-content:center;cursor:pointer;border-radius:6px;flex:none;
  transition:all .12s ease}
.mzai-thread:hover .mzai-thread-act{opacity:1}
/* Hidden row actions must not reserve width and truncate the title early.
   Touch (no hover) keeps them visible; focus still reveals them for keyboards. */
@media (hover:hover){.mzai-thread:not(:hover):not(:focus-within) .mzai-thread-act{width:0;margin-left:-6px;overflow:hidden}}
.mzai-thread-act:hover{background:var(--danger);color:var(--danger-badge-ink)}
.mzai-side-empty{padding:12px;font-size:12px;color:var(--muted);line-height:1.6}
.mzai-main{flex:1;display:flex;flex-direction:column;min-width:0;min-height:0}
.mzai-log{flex:1;overflow-y:auto;padding:16px;display:flex;flex-direction:column;gap:14px;overscroll-behavior:contain}
.mzai-msg-wrap{display:flex;gap:8px;align-items:flex-start;max-width:100%}
.mzai-msg{display:flex;flex-direction:column;gap:6px;max-width:80%}
.mzai-msg.user{align-self:flex-end;align-items:flex-end;max-width:85%}
.mzai-msg.assistant{align-self:flex-start;align-items:flex-start}
.mzai-avatar{width:32px;height:32px;border-radius:50%;flex-shrink:0;display:flex;align-items:center;justify-content:center;
  font-size:16px;font-weight:600;overflow:hidden}
/* MeshaLogo renders its own brand disc (matching the sidebar brand logo),
   so the assistant avatar container stays a neutral, disc-free wrapper. */
.mzai-msg.assistant .mzai-avatar{background:transparent;border:0}
.mzai-bub{padding:12px 14px;border-radius:16px;font-size:13px;line-height:1.6;white-space:pre-wrap;word-break:break-word}
.mzai-msg.user .mzai-bub{background:var(--brand);color:var(--on-brand);border-bottom-right-radius:4px;
  box-shadow:0 2px 8px rgba(var(--palette-common-blackChannel) / 0.1)}
.mzai-msg.assistant .mzai-bub{background:var(--panel-2);color:var(--ink);border:1px solid var(--line);
  border-bottom-left-radius:4px;box-shadow:0 2px 8px rgba(var(--palette-common-blackChannel) / 0.05)}
/* Tinted, not solid red: white on --danger fails 4.5:1 for body text. */
.mzai-msg.error .mzai-bub{background:color-mix(in srgb,var(--danger) 14%,var(--panel-2));color:var(--ink);
  border:1px solid color-mix(in srgb,var(--danger) 55%,transparent)}
.mzai-caret{display:inline-block;width:6px;height:14px;margin-left:2px;background:var(--brand);
  vertical-align:text-bottom;animation:mzai-type-caret .6s steps(1) infinite;border-radius:1px}
@keyframes mzai-type-caret{0%,49%{opacity:1}50%,100%{opacity:0}}
.mzai-chart{margin:8px 0 2px;padding:10px 12px;background:var(--panel-2);border:1px solid var(--line);
  border-radius:14px;max-width:100%;overflow:hidden;box-sizing:border-box;width:100%}
.mzai-chart-title{font-size:12px;font-weight:600;color:var(--muted);margin:0 0 8px;
  letter-spacing:.01em;white-space:normal}
.mzai-cites{display:flex;flex-wrap:wrap;gap:6px;margin-top:2px}
.mzai-cite{display:inline-flex;align-items:center;gap:5px;font-size:11px;font-weight:500;
  border:1px solid var(--line);border-radius:20px;padding:4px 11px;color:var(--muted);background:var(--panel);
  transition:all .15s ease}
.mzai-cite b{color:var(--ink);font-weight:600}
.mzai-cite.tier-cube{border-color:var(--brand-l);background:linear-gradient(135deg,var(--brand-soft),var(--brand-soft));
  color:var(--brand-d);box-shadow:0 0 12px var(--brand-soft)}
.mzai-cite.tier-cube b{color:var(--brand-d)}
.mzai-foot{display:flex;align-items:center;flex-wrap:wrap;gap:10px;font-size:11px;color:var(--muted);margin-top:4px}
.mzai-mode{display:inline-flex;align-items:center;gap:4px}
.mzai-mode.degraded{color:var(--danger)}
.mzai-fb{display:flex;align-items:center;gap:3px;margin-left:auto}
.mzai-fb button{width:28px;height:28px;border:1px solid var(--line);background:var(--bg);border-radius:8px;
  color:var(--muted);display:flex;align-items:center;justify-content:center;cursor:pointer;
  transition:all .12s ease}
.mzai-fb button:hover{background:var(--sidebar-2);color:var(--ink);border-color:var(--line2)}
.mzai-fb button.on-up{background:linear-gradient(135deg,var(--brand-soft),var(--brand-soft));
  border-color:var(--brand-l);color:var(--brand-d)}
.mzai-fb button.on-down{background:var(--error-soft);
  border-color:var(--danger);color:var(--error-ink)}
.mzai-reason{display:flex;gap:6px;margin-top:6px}
.mzai-reason input{flex:1;font:inherit;font-size:11.5px;border:1px solid var(--line);border-radius:8px;padding:6px 9px;
  background:var(--panel);color:var(--ink);outline:0;transition:border-color .15s ease}
.mzai-reason input:focus{border-color:var(--brand)}
.mzai-reason button{border:none;background:var(--brand);color:var(--on-brand);border-radius:8px;padding:6px 14px;
  font:inherit;font-size:11px;font-weight:600;cursor:pointer;transition:all .15s ease}
.mzai-reason button:hover{background:var(--brand-d)}
.mzai-suggestbar{display:flex;justify-content:flex-start;padding:0 16px 8px}
.mzai-suggestbar button{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);
  background:var(--panel-2);color:var(--ink);border-radius:10px;padding:7px 10px;font:inherit;
  font-size:12px;font-weight:600;cursor:pointer;transition:all .15s ease}
.mzai-suggestbar button:hover,.mzai-suggestbar button[aria-expanded="true"]{border-color:var(--brand-l);
  background:linear-gradient(135deg,var(--brand-soft),var(--brand-soft));color:var(--brand-d)}
.mzai-starters{display:flex;flex-wrap:wrap;gap:8px;padding:0 16px 12px;max-height:164px;
  overflow-y:auto;overscroll-behavior:contain}
.mzai-starters button{display:flex;align-items:center;gap:6px;border:1px solid var(--line);
  background:var(--panel-2);color:var(--ink);border-radius:20px;padding:8px 14px;font:inherit;
  font-size:12px;cursor:pointer;text-align:left;transition:all .15s ease}
.mzai-starters button:before{content:"✨";font-size:13px}
.mzai-starters button:hover{border-color:var(--brand-l);background:linear-gradient(135deg,var(--brand-soft),var(--brand-soft))}
.mzai-starters button:disabled{opacity:.5;cursor:not-allowed}
.mzai-form{display:flex;gap:10px;align-items:flex-end;padding:12px 14px;border-top:1px solid var(--line);
  background:var(--panel-2)}
.mzai-form textarea{flex:1;resize:none;border:1px solid var(--line);border-radius:12px;padding:10px 13px;
  font:inherit;font-size:13px;background:var(--panel);color:var(--ink);outline:0;max-height:120px;
  transition:border-color .15s ease}
.mzai-form textarea:focus{border-color:var(--brand);box-shadow:0 0 0 3px var(--ring)}
.mzai-form textarea::placeholder{color:var(--muted)}
.mzai-send{width:40px;height:40px;flex:none;border-radius:12px;border:none;background:var(--brand);
  color:var(--on-brand);display:flex;align-items:center;justify-content:center;cursor:pointer;
  transition:all .15s cubic-bezier(.34,.1,.64,.9);box-shadow:0 2px 6px rgba(var(--palette-common-blackChannel) / 0.1)}
.mzai-send:hover:not(:disabled){background:var(--brand-d);transform:translateY(-2px);box-shadow:0 4px 12px rgba(var(--palette-common-blackChannel) / 0.15)}
.mzai-send:active:not(:disabled){transform:translateY(0)}
.mzai-send:disabled{opacity:.5;cursor:not-allowed}
.mzai-send.stop{background:var(--brand);border:none;cursor:pointer}
.mzai-send.stop:hover{background:var(--brand-d);transform:translateY(-2px)}
.mzai-send .mzai-goat-walking{width:18px;height:18px}
.mzai-banner{margin:0 16px 12px;padding:10px 12px;border-radius:12px;font-size:12px;line-height:1.5;
  display:flex;align-items:center;gap:8px;animation:mzai-slide-up .3s ease}
.mzai-banner.err{background:var(--error-soft);
  border:1px solid color-mix(in srgb,var(--error) 36%,transparent);color:var(--error-ink)}
.mzai-banner.warn{background:var(--warning-soft);
  border:1px solid color-mix(in srgb,var(--warning) 36%,transparent);color:var(--warning-ink)}
@keyframes mzai-slide-up{from{opacity:0;transform:translateY(4px)}to{opacity:1;transform:translateY(0)}}
.mzai-skel{display:flex;gap:5px;padding:12px 14px}
.mzai-skel span{width:6px;height:6px;border-radius:50%;background:var(--muted);animation:mzai-bounce .8s infinite}
.mzai-skel span:nth-child(1){animation-delay:0s}
.mzai-skel span:nth-child(2){animation-delay:.12s}
.mzai-skel span:nth-child(3){animation-delay:.24s}
@keyframes mzai-bounce{0%,100%{opacity:.4;transform:translateY(0)}50%{opacity:1;transform:translateY(-4px)}}
.mzai-progress{display:flex;align-items:center;gap:6px;flex-wrap:wrap}
.mzai-progress .mzai-skel{padding:12px 8px 12px 14px}
.mzai-progress-label{font-size:12px;color:var(--muted);letter-spacing:.01em}
@media (max-width:600px){
  .mzai-side{width:140px}
  .mzai-icon,.mzai-send{min-height:40px}
  .mzai-msg{max-width:90%}
  .mzai-msg.user{max-width:92%}
  .mzai-log{padding:12px}
  .mzai-form{padding:10px}
}
/* ---- composer + phone fixes (kept last so they win over earlier rules) ---- */
.mzai-form textarea::placeholder{white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.mzai-form textarea{min-width:0;line-height:1.4}
.mzai-newbtn{white-space:nowrap}
.mzai-scrim{display:none}
@media (max-width:620px){
  .mzai-root .mzai-hide-mobile{display:none}
  .mzai-side{width:min(78vw,280px)}
  .mzai-scrim{display:block;position:absolute;inset:0;z-index:1;border:0;padding:0;background:rgba(var(--palette-common-blackChannel) / 0.45)}
  .mzai-form{gap:6px;padding:8px 10px}
  .mzai-tool{width:36px;height:40px}
  .mzai-send{width:40px}
  .mzai-root .mzai-form textarea{font-size:16px;padding:9px 11px}
  .mzai-head{padding:10px 12px;gap:8px}
  .mzai-hbtns{gap:4px}
  .mzai-htext small{display:none}
  .mzai-starters{flex-wrap:nowrap;overflow-x:auto;padding:4px 12px 10px;scrollbar-width:none}
  .mzai-starters button{flex:none;max-width:78vw;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
}
.mzai-md ul{list-style:disc}.mzai-md ol{list-style:decimal}.mzai-md li::marker{color:var(--muted)}
/* calmer emphasis: same Mesha green tokens, tinted instead of solid fills */
.mzai-msg.user .mzai-bub{background:var(--brand-soft);color:var(--ink);border:1px solid color-mix(in srgb,var(--brand) 45%,transparent);box-shadow:none}
.mzai-msg.user .mzai-thumb-open{border-color:var(--line);background:var(--panel);color:var(--ink)}
.mzai-newbtn{background:transparent;color:var(--brand);border-color:color-mix(in srgb,var(--brand) 55%,transparent);box-shadow:none}
.mzai-newbtn:hover{background:var(--brand-soft)}
.mzai-thread.mzai-confirming{background:color-mix(in srgb,var(--danger) 12%,transparent);cursor:default}
.mzai-confirm{display:flex;flex-wrap:wrap;align-items:center;gap:6px;width:100%;font-size:12px;color:var(--ink)}
.mzai-confirm>span{flex:1 1 100%}
.mzai-confirm button{font:inherit;font-size:11.5px;padding:3px 9px;border-radius:6px;cursor:pointer}
.mzai-confirm-yes{border:0;background:var(--danger);color:var(--palette-common-white)}
.mzai-confirm-no{border:1px solid var(--line);background:transparent;color:var(--ink)}
/* Phones: the launcher was hidden (no other entry point) — show a compact one,
   raised clear of browser/webview bottom toolbars. */
@media (max-width:620px){
  .mzai-root.mzai-closed{display:block;right:14px !important;bottom:calc(88px + env(safe-area-inset-bottom)) !important}
  .mzai-closed .mzai-bubble{width:44px;height:44px}
  .mzai-closed .mzai-goat-icon{width:22px;height:22px}
  /* Phone taps: header (threads / minimise / close) and composer controls meet 44px. */
  .mzai-icon,.mzai-tool,.mzai-send{width:44px;height:44px}
}
/* draggable launcher: inline left/top win over the corner defaults */
.mzai-root.mzai-closed.mzai-free{right:auto !important;bottom:auto !important;transition:none}
.mzai-closed .mzai-bubble{touch-action:none;user-select:none;-webkit-user-select:none}
.mzai-steps{margin:2px 0 8px;font-size:12.5px;line-height:1.45;color:var(--muted);max-width:100%}
.mzai-steps-head{display:inline-flex;align-items:center;gap:5px;border:0;background:transparent;color:var(--muted);
  font:inherit;font-size:12.5px;font-weight:500;padding:3px 0;cursor:pointer}
.mzai-steps-head:hover{color:var(--ink)}
.mzai-steps.live .mzai-steps-head{cursor:default;background:linear-gradient(90deg,var(--muted) 0%,var(--ink) 50%,var(--muted) 100%);
  background-size:200% 100%;-webkit-background-clip:text;background-clip:text;color:transparent;animation:mzai-shimmer 1.8s linear infinite}
@keyframes mzai-shimmer{from{background-position:100% 0}to{background-position:-100% 0}}
.mzai-steps-chev{font-size:12px;opacity:.8}
.mzai-steps ol{list-style:none;margin:2px 0 0;padding:0;display:flex;flex-direction:column;gap:1px}
.mzai-steps li{display:flex;align-items:flex-start;gap:7px;padding:1px 0;min-width:0}
.mzai-step-tx{min-width:0;overflow-wrap:anywhere}
.mzai-steps li.mzai-step-more{padding-left:19px;font-size:11.5px;opacity:.7}
.mzai-step-ic{flex:none;width:12px;height:12px;margin-top:3px;display:inline-flex;align-items:center;justify-content:center;color:var(--brand)}
.mzai-steps li.now{color:var(--ink)}
.mzai-steps li.now .mzai-step-ic{border:1.5px solid color-mix(in srgb,var(--brand) 35%,transparent);border-top-color:var(--brand);
  border-radius:50%;animation:mzai-spin .8s linear infinite}
@keyframes mzai-spin{to{transform:rotate(360deg)}}
@media (prefers-reduced-motion:reduce){.mzai-steps.live .mzai-steps-head,.mzai-steps li.now .mzai-step-ic{animation:none}}
.mzai-send.stop{background:var(--panel);color:var(--ink);border:1.5px solid var(--brand)}
.mzai-send.stop:hover{background:var(--brand-soft);transform:none}
/* hidden file input must not take a flex gap slot; tighter composer so the
   desktop placeholder fits at normal panel width with the chats list open */
.mzai-form input[type=file]{display:none}
@media (min-width:621px){
  .mzai-view-normal .mzai-side{width:176px}
  .mzai-form{gap:8px;padding:12px}
  .mzai-form textarea{padding:10px 12px}
}
/* Code blocks follow the app theme: dark (GitHub) palette stays on the dark
   console; on :root.light they use the light panel tokens. */
:root.light .mzai-code{background:var(--panel-2)}
:root.light .mzai-code-head{background:var(--sidebar);color:var(--muted);border-bottom-color:var(--line)}
:root.light .mzai-code pre{color:var(--ink)}
:root.light .mzai-code .mzai-copy-ic{color:var(--muted)}
:root.light .mzai-code .mzai-copy-ic:hover{color:var(--ink)}
:root.light .mzai-code .hljs-comment,:root.light .mzai-code .hljs-quote{color:var(--muted)}
:root.light .mzai-code .hljs-keyword,:root.light .mzai-code .hljs-selector-tag,:root.light .mzai-code .hljs-literal,:root.light .mzai-code .hljs-type{color:var(--danger)}
:root.light .mzai-code .hljs-string,:root.light .mzai-code .hljs-regexp,:root.light .mzai-code .hljs-addition{color:var(--teal)}
:root.light .mzai-code .hljs-number,:root.light .mzai-code .hljs-symbol,:root.light .mzai-code .hljs-bullet,:root.light .mzai-code .hljs-attr,:root.light .mzai-code .hljs-attribute,:root.light .mzai-code .hljs-variable,:root.light .mzai-code .hljs-template-variable,:root.light .mzai-code .hljs-property{color:var(--info)}
:root.light .mzai-code .hljs-title,:root.light .mzai-code .hljs-section{color:var(--purple)}
:root.light .mzai-code .hljs-built_in,:root.light .mzai-code .hljs-name,:root.light .mzai-code .hljs-tag{color:var(--brand-d)}
:root.light .mzai-code .hljs-meta{color:var(--warn)}
:root.light .mzai-code .hljs-deletion{color:var(--danger)}
${WATCH_CSS}
`}</style>
  );
}

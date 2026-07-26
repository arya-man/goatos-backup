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


// The Twemoji goat artwork, shared by the avatar and the walking-button variant.
function GoatArt(): ReactElement {
  return (
    <>
      <path fill="#FFAC33" d="M7.44 7.503c-1-4 3.687-6 8-4 .907.421.948 1.316 0 1-3-1-6 1-4 4 1.109 1.664-3.233 2.068-4-1z" />
      <path fill="#FFCC4D" d="M6.136 5.785c-1-4 3.687-6 8-4 .907.421.949 1.316 0 1-3-1-6 1-4 4 1.11 1.664-3.233 2.067-4-1z" />
      <path fill="#E1E8ED" d="M5 14.785c0 4-2 4.827-2 4 0-2-1 0-1-1v-3c0-1.657.671-3 1.5-3s1.5 1.343 1.5 3z" />
      <path fill="#CCD6DD" d="M35.159 10.49c-.68-1.643-2.313-2.705-4.159-2.705-.553 0-1 .448-1 1s.447 1 1 1c1.034 0 1.941.577 2.312 1.471.341.824.168 1.758-.455 2.647-.984-1.506-2.602-2.618-4.856-2.618-2.391 0-7.279.714-10.828 1.289-.052-.094-.105-.188-.172-.289-2-3-4-8.157-7-8.157-4 0-10 4.986-10 9.157 0 2.544 5.738 2.929 7.486 2.988.697 1.43 1.414 2.934 2.232 4.33.066.205.155.429.282.683 3 6 3.119 14.5 4.5 14.5s2.5-4.857 2.5-9c0-.151-.004-.299-.007-.447 3.126.649 6.607.322 9.677-.61 1.448 5.045 1.77 10.058 2.83 10.058 1.342 0 2.433-8.818 2.494-13.12C33.316 21.226 34 19.51 34 17.785c0-.605-.086-1.23-.248-1.843 1.614-1.644 2.143-3.676 1.407-5.452z" />
      <circle fill="#292F33" cx="7" cy="9.285" r="1" />
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
    <span
      aria-label="Mesha"
      role="img"
      className={className}
      style={{
        width,
        height: width,
        flex: "none",
        display: "grid",
        placeItems: "center",
        borderRadius: "50%",
        border: "1.5px solid var(--brand)",
        background: "var(--brand-soft)",
        color: "var(--brand)",
        fontWeight: 800,
        fontSize: Math.round(width * 0.47),
        lineHeight: 1,
        fontFamily: "var(--f)",
      }}
    >
      मे
    </span>
  );
}

export function CeoAiStyles(): ReactElement {
  return (
    <style>{`
.mzai-root{position:fixed;bottom:24px;right:24px;z-index:80;font-family:var(--f);transition:all .3s cubic-bezier(.34,.1,.64,.9);
  overscroll-behavior:contain}
.mzai-bubble{width:56px;height:56px;border-radius:50%;border:2px solid var(--brand);
  background:var(--brand);color:#fff;display:flex;align-items:center;justify-content:center;
  cursor:pointer;box-shadow:0 4px 12px rgba(0,0,0,.15);transition:transform .2s cubic-bezier(.34,.1,.64,.9),box-shadow .2s ease}
.mzai-bubble:hover{transform:translateY(-4px);box-shadow:0 8px 20px rgba(0,0,0,.2)}
.mzai-bubble:active{transform:translateY(-2px)}
.mzai-goat-icon{width:28px;height:28px}
@media (prefers-reduced-motion: reduce) {
  .mzai-bubble{transition:none}
  .goat-bubble{animation:none !important}
  .goat-eye-left,.goat-eye-right,.goat-ear{animation:none !important}
}
.mzai-panel{display:flex;flex-direction:column;height:100%;background:var(--panel);
  border:1px solid var(--line);border-radius:20px;overflow:hidden;
  box-shadow:0 20px 60px rgba(0,0,0,.2),0 0 1px rgba(0,0,0,.1);
  animation:mzai-panel-open .3s cubic-bezier(.34,.1,.64,.9);overscroll-behavior:contain}
@keyframes mzai-panel-open{from{opacity:0;transform:scale(.8) translateY(8px)}to{opacity:1;transform:scale(1) translateY(0)}}
.mzai-head{display:flex;align-items:center;gap:12px;padding:14px 16px;border-bottom:1px solid var(--line);
  background:linear-gradient(135deg,var(--panel) 0%,var(--panel-2) 100%)}
/* Neutral wrapper: MeshaLogo renders its own brand disc (matching the sidebar
   brand logo), so the mark container must not add a second disc/ring. */
.mzai-mark{display:flex;align-items:center;justify-content:center;flex:none}
.mzai-mark .mzai-goat-icon{width:20px;height:20px}
.mzai-htext{display:flex;flex-direction:column;min-width:0;flex:1}
.mzai-htext b{font-size:14px;font-weight:600;color:var(--ink);line-height:1.2}
.mzai-htext small{font-size:11.5px;color:var(--muted);margin-top:2px}
.mzai-hbtns{display:flex;align-items:center;gap:6px}
.mzai-icon{width:36px;height:36px;border-radius:10px;border:1px solid var(--line);
  background:var(--bg);color:var(--ink);display:flex;align-items:center;justify-content:center;
  cursor:pointer;transition:all .15s ease}
.mzai-icon:hover{background:var(--sidebar-2);border-color:var(--brand-l)}
.mzai-icon:active{transform:scale(.95)}
.mzai-icon .ic{width:18px;height:18px}
.mzai-icon[aria-pressed="true"]{background:var(--brand-soft);border-color:var(--brand);color:var(--brand-d)}
.mzai-body{display:flex;flex:1;min-height:0}
.mzai-side{width:200px;flex:none;border-right:1px solid var(--line);background:var(--panel-2);
  display:flex;flex-direction:column;min-height:0}
.mzai-side.mzai-hide{display:none}
.mzai-side-head{display:flex;align-items:center;justify-content:space-between;padding:12px 12px 10px}
.mzai-side-head span{font-size:10px;font-weight:700;letter-spacing:.08em;text-transform:uppercase;color:var(--muted)}
.mzai-newbtn{display:flex;align-items:center;gap:5px;border:1px solid var(--brand-l);background:linear-gradient(135deg,#7CCB45 0%,#69BA37 100%);
  color:#fff;border-radius:8px;padding:6px 9px;font:inherit;font-size:11px;font-weight:600;cursor:pointer;
  transition:all .15s ease;box-shadow:0 2px 6px rgba(0,0,0,.08)}
.mzai-newbtn:hover{transform:translateY(-1px);box-shadow:0 4px 12px rgba(0,0,0,.12)}
.mzai-newbtn .ic{width:13px;height:13px}
.mzai-threads{flex:1;overflow-y:auto;padding:6px 8px 10px;overscroll-behavior:contain}
.mzai-thread{display:flex;align-items:center;gap:6px;border-radius:10px;padding:8px 8px;cursor:pointer;
  transition:all .12s ease}
.mzai-thread:hover{background:var(--sidebar-2)}
.mzai-thread.mzai-on{background:linear-gradient(135deg,rgba(124,203,69,.12),rgba(105,186,55,.08));
  border-left:3px solid var(--brand)}
.mzai-thread .mzai-tt{flex:1;min-width:0;font-size:12px;color:var(--ink);overflow:hidden;
  text-overflow:ellipsis;white-space:nowrap}
.mzai-thread input{flex:1;min-width:0;font:inherit;font-size:12px;border:1px solid var(--brand);
  border-radius:8px;padding:4px 8px;background:var(--panel);color:var(--ink);outline:0}
.mzai-thread-act{opacity:0;width:24px;height:24px;border:0;background:transparent;color:var(--muted);
  display:flex;align-items:center;justify-content:center;cursor:pointer;border-radius:6px;flex:none;
  transition:all .12s ease}
.mzai-thread:hover .mzai-thread-act{opacity:1}
.mzai-thread-act:hover{background:var(--danger);color:#fff}
.mzai-thread-act .ic{width:13px;height:13px}
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
.mzai-msg.user .mzai-bub{background:var(--brand);color:#fff;border-bottom-right-radius:4px;
  box-shadow:0 2px 8px rgba(0,0,0,.1)}
.mzai-msg.assistant .mzai-bub{background:var(--panel-2);color:var(--ink);border:1px solid var(--line);
  border-bottom-left-radius:4px;box-shadow:0 2px 8px rgba(0,0,0,.05)}
.mzai-msg.error .mzai-bub{background:var(--danger);color:#fff;border:0;box-shadow:0 2px 8px rgba(0,0,0,.15)}
.mzai-caret{display:inline-block;width:6px;height:14px;margin-left:2px;background:var(--brand);
  vertical-align:text-bottom;animation:mzai-type-caret .6s steps(1) infinite;border-radius:1px}
@keyframes mzai-type-caret{0%,49%{opacity:1}50%,100%{opacity:0}}
.mzai-chart{margin:8px 0 2px;padding:10px 12px;background:var(--panel-2);border:1px solid var(--line);
  border-radius:14px;max-width:100%;overflow:hidden}
.mzai-chart-title{font-size:11px;font-weight:600;color:var(--muted);margin:0 0 6px;
  letter-spacing:.01em}
.mzai-chart-svg{display:block;width:100%;height:auto}
.mzai-cites{display:flex;flex-wrap:wrap;gap:6px;margin-top:2px}
.mzai-cite{display:inline-flex;align-items:center;gap:5px;font-size:11px;font-weight:500;
  border:1px solid var(--line);border-radius:20px;padding:4px 11px;color:var(--muted);background:var(--panel);
  transition:all .15s ease}
.mzai-cite b{color:var(--ink);font-weight:600}
.mzai-cite.tier-cube{border-color:var(--brand-l);background:linear-gradient(135deg,rgba(124,203,69,.08),rgba(105,186,55,.04));
  color:var(--brand-d);box-shadow:0 0 12px rgba(124,203,69,.12)}
.mzai-cite.tier-cube b{color:var(--brand-d)}
.mzai-foot{display:flex;align-items:center;flex-wrap:wrap;gap:10px;font-size:11px;color:var(--muted);margin-top:4px}
.mzai-mode{display:inline-flex;align-items:center;gap:4px}
.mzai-mode .ic{width:12px;height:12px}
.mzai-mode.degraded{color:var(--danger)}
.mzai-fb{display:flex;align-items:center;gap:3px;margin-left:auto}
.mzai-fb button{width:28px;height:28px;border:1px solid var(--line);background:var(--bg);border-radius:8px;
  color:var(--muted);display:flex;align-items:center;justify-content:center;cursor:pointer;
  transition:all .12s ease}
.mzai-fb button:hover{background:var(--sidebar-2);color:var(--ink);border-color:var(--line2)}
.mzai-fb button.on-up{background:linear-gradient(135deg,rgba(124,203,69,.12),rgba(105,186,55,.08));
  border-color:var(--brand-l);color:var(--brand-d)}
.mzai-fb button.on-down{background:linear-gradient(135deg,rgba(239,68,68,.12),rgba(220,38,38,.08));
  border-color:var(--danger);color:var(--danger)}
.mzai-fb button .ic{width:13px;height:13px}
.mzai-reason{display:flex;gap:6px;margin-top:6px}
.mzai-reason input{flex:1;font:inherit;font-size:11.5px;border:1px solid var(--line);border-radius:8px;padding:6px 9px;
  background:var(--panel);color:var(--ink);outline:0;transition:border-color .15s ease}
.mzai-reason input:focus{border-color:var(--brand)}
.mzai-reason button{border:none;background:var(--brand);color:#fff;border-radius:8px;padding:6px 14px;
  font:inherit;font-size:11px;font-weight:600;cursor:pointer;transition:all .15s ease}
.mzai-reason button:hover{background:var(--brand-d)}
.mzai-suggestbar{display:flex;justify-content:flex-start;padding:0 16px 8px}
.mzai-suggestbar button{display:inline-flex;align-items:center;gap:6px;border:1px solid var(--line);
  background:var(--panel-2);color:var(--ink);border-radius:10px;padding:7px 10px;font:inherit;
  font-size:12px;font-weight:600;cursor:pointer;transition:all .15s ease}
.mzai-suggestbar button:hover,.mzai-suggestbar button[aria-expanded="true"]{border-color:var(--brand-l);
  background:linear-gradient(135deg,rgba(124,203,69,.08),rgba(105,186,55,.04));color:var(--brand-d)}
.mzai-suggestbar .ic{width:14px;height:14px}
.mzai-starters{display:flex;flex-wrap:wrap;gap:8px;padding:0 16px 12px;max-height:164px;
  overflow-y:auto;overscroll-behavior:contain}
.mzai-starters button{display:flex;align-items:center;gap:6px;border:1px solid var(--line);
  background:var(--panel-2);color:var(--ink);border-radius:20px;padding:8px 14px;font:inherit;
  font-size:12px;cursor:pointer;text-align:left;transition:all .15s ease}
.mzai-starters button:before{content:"✨";font-size:13px}
.mzai-starters button:hover{border-color:var(--brand-l);background:linear-gradient(135deg,rgba(124,203,69,.08),rgba(105,186,55,.04))}
.mzai-starters button:disabled{opacity:.5;cursor:not-allowed}
.mzai-form{display:flex;gap:10px;align-items:flex-end;padding:12px 14px;border-top:1px solid var(--line);
  background:var(--panel-2)}
.mzai-form textarea{flex:1;resize:none;border:1px solid var(--line);border-radius:12px;padding:10px 13px;
  font:inherit;font-size:13px;background:var(--panel);color:var(--ink);outline:0;max-height:120px;
  transition:border-color .15s ease}
.mzai-form textarea:focus{border-color:var(--brand);box-shadow:0 0 0 3px rgba(124,203,69,.1)}
.mzai-form textarea::placeholder{color:var(--muted)}
.mzai-send{width:40px;height:40px;flex:none;border-radius:12px;border:none;background:var(--brand);
  color:#fff;display:flex;align-items:center;justify-content:center;cursor:pointer;
  transition:all .15s cubic-bezier(.34,.1,.64,.9);box-shadow:0 2px 6px rgba(0,0,0,.1)}
.mzai-send:hover:not(:disabled){background:var(--brand-d);transform:translateY(-2px);box-shadow:0 4px 12px rgba(0,0,0,.15)}
.mzai-send:active:not(:disabled){transform:translateY(0)}
.mzai-send:disabled{opacity:.5;cursor:not-allowed}
.mzai-send.stop{background:var(--brand);border:none;cursor:pointer}
.mzai-send.stop:hover{background:var(--brand-d);transform:translateY(-2px)}
.mzai-send .ic{width:18px;height:18px}
.mzai-send .mzai-goat-walking{width:18px;height:18px}
.mzai-banner{margin:0 16px 12px;padding:10px 12px;border-radius:12px;font-size:12px;line-height:1.5;
  display:flex;align-items:center;gap:8px;animation:mzai-slide-up .3s ease}
.mzai-banner.err{background:linear-gradient(135deg,rgba(239,68,68,.1),rgba(220,38,38,.06));
  border:1px solid rgba(239,68,68,.3);color:var(--danger)}
.mzai-banner.warn{background:linear-gradient(135deg,rgba(245,158,11,.1),rgba(217,119,6,.06));
  border:1px solid rgba(245,158,11,.3);color:var(--ink)}
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
`}</style>
  );
}

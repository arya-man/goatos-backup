/**
 * The WebView tap floor (apps/admin-web/AGENTS.md "Mobile webview rules"): every control is at least
 * 44px at phone width. Theme value for sx (`minHeight: TAP_MIN` = 44px), so no component reads the
 * `--tap-min` CSS variable (J1B P2-1, FIXJ7; ratchet `css-var-token`). AppBaseline emits the same
 * value as `--tap-min` for its element-level rules.
 */
export const TAP_MIN = 44;

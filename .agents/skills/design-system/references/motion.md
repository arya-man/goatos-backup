# Motion constants

Measured from the reference (`scratchpad/apex-motion-measurements.json`) and fixed in the kit.
Every animation has a `prefers-reduced-motion: reduce` branch that renders the final state
immediately. The visual lanes freeze all CSS animation/transition before capturing, so a
component that hides its final state behind a transition end event will screenshot blank —
gate visibility on state, not on `transitionend`.

| Surface | Duration | Easing | Stagger / notes |
|---|---|---|---|
| Page enter (`PageEnter` → `PageEnterItem`) | 400ms | ease-out | 60ms apart, fade + 8px rise; only direct children stagger |
| Bar list fill (`BarList`) | 600ms | ease-out | 40ms per row, grows from the zero axis |
| Chart draw-in (bars grow, lines/areas draw, donut sweep) | ~800ms | easeInOutSine | ~150ms per series |
| Chart hover | 150ms | ease | hovered mark +15% light, siblings dim; tooltip fade + scale, follows pointer |
| Count-up (`CountUp`) | 800ms | ease-out | starts on mount / when visible |
| Tabs indicator (`AnimatedTabs`) | 250ms | ease | slides between tabs; panel crossfade, no height jump |
| Dialog | 200ms | ease-out | scale from 0.96 + fade; backdrop fade |
| Drawer / Sheet | 260ms | ease-out | slide from edge; backdrop fade |
| Popover / Menu / Tooltip | 150ms | ease | fade + 4px translate from anchor |
| Button press | 100ms | ease | scale 0.98; hover lift; icon buttons bg fade only |
| Skeleton shimmer | 1.5s loop | linear | left → right sweep |
| Theme toggle icon | 200ms | ease | 90° rotate + cross-fade |
| Row hover | 120ms | ease | background lift only — no transform, no layout change |

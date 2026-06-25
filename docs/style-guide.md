# MuninnDB UI Style Guide

> Formal style guide derived from the MuninnDB web UI source
> (`scrypster/muninndb`, `web/`). Every value below is taken directly from
> `web/static/css/{theme,base,components,app}.css` and
> `web/templates/index.html`. Treat this document as the canonical reference
> for any new MuninnDB surface; the CSS files remain the executable source of
> truth — when this guide and the CSS disagree, the CSS wins.

---

## 1. Design principles

MuninnDB is an operator console for a long-term memory database. The UI is
read-mostly, dense, and dark-first. Five principles govern every decision:

1. **Dark by default, light as opt-in.** The dark palette is the reference;
   the light palette is a mechanical inversion. Never design a component
   against the light theme first.
2. **Surfaces, not shadows.** Depth is communicated by background tint and
   1px borders, never by drop shadows (the single exception is the
   hover-lift on polished cards, and even there the shadow is a faint
   primary-tinted glow, not a neutral drop).
3. **One accent does the work.** `--primary` (cyan) is the only color that
   signals "interactive / selected / live." `--accent` (purple) is reserved
   for the *active tab* and the *selected vault* — two places that must read
   as "current context," not "clickable."
4. **Density over whitespace.** Spacing tops out at 1.5rem of page padding;
   internal card padding is 1–1.25rem. The grid is built to show many things
   at once on a 1400px max-width canvas.
5. **Motion is feedback, not decoration.** Every transition is 100–300ms and
   tied to a state change (hover, focus, expand, toast入场). No ambient
   animation except the SSE disconnect pulse.

---

## 2. Technology stack

| Layer | Choice | Notes |
|---|---|---|
| CSS framework | Tailwind CSS 3.4 | `darkMode: 'class'`; used only for utilities. Component classes are hand-written in `components.css`. |
| Build | Vite 6 | `web/vite.config.js`; output to `static/dist/app.css`. |
| PostCSS | tailwindcss + autoprefixer | `postcss.config.js`. |
| Interactivity | Alpine.js 3.14 | Single `muninnApp` root component on `<body>`. |
| Charts | Chart.js 4.4 | Vendored. |
| Graphs | Cytoscape.js 3.30 + fcose layout | Vendored; entity graph only. |
| Icons | Inline SVG, Lucide-style | `viewBox="0 0 24 24"`, `stroke="currentColor"`, `stroke-width="2"`. Never an icon font. |
| Fonts | Inter (system-ui fallback) | Loaded via `font-family` stack; no web-font import in the CSS. |

---

## 3. Color system

All colors are CSS custom properties defined in `theme.css`. Two complete
palettes exist — dark (the `:root` default) and light (applied by adding the
`light` class to `<html>` via a FOUC-prevention script that reads
`localStorage['muninnTheme']`).

### 3.1 Dark palette (default)

| Token | Hex | Role |
|---|---|---|
| `--bg-base` | `#0a0a0a` | Page background; near-black. |
| `--bg-surface` | `#1a1a2e` | Card / sidebar background. |
| `--bg-elevated` | `#16213e` | Inputs, dropdowns, hovers. |
| `--bg-card` | `#1e2a45` | Highest card surface (reserved). |
| `--border` | `#2a2a4a` | Every 1px divider on the page. |
| `--text-primary` | `#e2e8f0` | Body copy, headings. |
| `--text-secondary` | `#94a3b8` | Labels, secondary text, inactive nav. |
| `--text-muted` | `#64748b` | Placeholders, disabled, version strings. |
| `--primary` | `#06b6d4` | Cyan — interactive, selected nav, links, focus ring. |
| `--primary-dark` | `#0891b2` | Gradient end-stop for primary buttons. |
| `--accent` | `#a855f7` | Purple — active tab, selected vault only. |
| `--accent-dark` | `#9333ea` | Gradient end-stop for confidence/progress fills. |
| `--success` | `#22c55e` | Connected, OK, badge-success. |
| `--danger` | `#ef4444` | Destructive, error, disconnected. |
| `--warning` | `#f59e0b` | Caution badge. |
| `--info` | `#3b82f6` | Informational badge. |
| `--yellow` | `#eab308` | Idle state badge. |

### 3.2 Light palette

When `html.light` is present, the same tokens are overridden:

| Token | Light value |
|---|---|
| `--bg-base` | `#fafafa` |
| `--bg-surface` | `#ffffff` |
| `--bg-elevated` | `#f1f5f9` |
| `--bg-card` | `#f8fafc` |
| `--border` | `#e2e8f0` |
| `--text-primary` | `#0f172a` |
| `--text-secondary` | `#475569` |
| `--text-muted` | `#94a3b8` |

The accent, primary, success, danger, warning, and info hues are **not**
overridden — they carry across themes unchanged. This is intentional: the
brand colors are the same in both modes; only the neutrals invert.

### 3.3 Usage rules

- **Never introduce a new hex value.** If you need a color not in the table,
  add a token to `theme.css` first, then reference the token.
- **Never use `--accent` for a button or link.** It marks "current state"
  (active tab, selected vault). Using it for "clickable" collapses the
  visual grammar.
- **Translucent overlays on dark** use `rgba(255,255,255,0.05–0.18)` for
  hover washes; **on light** use `rgba(0,0,0,0.04)`. Both are present in
  `components.css` under `.tab-btn:hover` and `.seg-btn:hover`.
- **Primary-tinted washes** (the "selected" look) use `rgba(6,182,212,0.06)`
  for hover and `rgba(6,182,212,0.1)` for active. This 0.06 → 0.1 ramp is
  the only sanctioned way to signal "this is the current thing."

---

## 4. Typography

| Element | Size | Weight | Other |
|---|---|---|---|
| `.page-title` | `1.5rem` | 700 | `letter-spacing: -0.01em`, `color: --text-primary`, `margin: 0`. |
| `.stat-card .stat-value` | `1.875rem` | 700 | `color: --primary`. |
| `.stat-card .stat-label` | `0.875rem` | 400 | `color: --text-secondary`. |
| Body | inherited | 400 | `font-family: 'Inter', system-ui, -apple-system, sans-serif`. |
| `.sidebar-item` | `0.875rem` | 500 | `color: --text-secondary` → `--primary` on hover/active. |
| `.form-group label` | `0.875rem` | 500 | `color: --text-secondary`. |
| `.tab-btn` | `0.8125rem` | 500 (600 active) | `color: --text-muted` → `--accent` active. |
| `.btn-sm` | `0.8125rem` | inherited | Compact modifier. |
| `.badge-*` | `0.75rem` | 600 | Always pill-shaped. |
| `.sidebar-version-label` | `0.625rem` | 400 | `opacity: 0.5`, `user-select: none`. |
| `.vault-picker-heading` | `0.6875rem` | 700 | `letter-spacing: 0.08em`, uppercase, `--text-muted`. |
| `.tip::after` (tooltip) | `0.6875rem` | 400 | `line-height: 1.55`. |

### Type rules

- **Three text colors only.** Primary (`--text-primary`) for content,
  secondary (`--text-secondary`) for chrome and labels, muted
  (`--text-muted`) for disabled and tertiary metadata. Anything that would
  be "lighter still" uses `opacity` on a muted token, not a new color.
- **Numerals in stat cards are cyan.** A big number that is not cyan is a
  bug — it either means the value is loading or the wrong class was applied.
- **Uppercase is rare and deliberate.** Used only for the picker heading
  (`VAULT`-style micro-labels). Body text, buttons, and nav are
  sentence-case.
- **Font weight escalates with emphasis, never italicizes.** Italic is not
  used anywhere in the UI.

---

## 5. Spacing scale

The codebase does not use a Tailwind utility scale for layout; it uses rem
literals inside component classes. The de-facto scale, in descending
frequency:

| Value | Where it appears |
|---|---|
| `0.5rem` | Small gaps (`gap-2`), badge vertical padding, tooltip inner pad. |
| `0.75rem` | Button vertical padding, `btn-sm` horizontal pad, input padding. |
| `0.875rem` | Sidebar item gap (icon↔label), sidebar logo gap. |
| `1rem` | Memory-card padding, button horizontal padding, modal-border radius. |
| `1.25rem` | `.card-polished` and `.stat-card` padding. |
| `1.5rem` | `.app-main` page padding, modal padding, toast offset from viewport. |
| `2rem`+ | Reserved — almost never used. Density over breathing room. |

### Layout primitives

- **`.app-layout`** is a `display: flex` row, `min-height: 100vh`.
- **`.app-sidebar`** is `width: 56px` collapsed, `220px` expanded (toggle on
  `.expanded`). `position: sticky; top: 0`. Background is a 95% opaque
  base tint with `backdrop-filter: blur(12px)`.
- **`.app-main`** is `flex: 1`, `padding: 1.5rem`, `max-width: 1400px`,
  `overflow-y: auto`. The max-width is a hard ceiling — wider screens get
  margin, not wider content.
- **`.activity-grid`** collapses to a single column under 768px. This is
  the only responsive breakpoint in the codebase.

---

## 6. Component catalog

### 6.1 Cards

Three card variants, each with a distinct job:

| Class | Padding | Radius | Border | Use |
|---|---|---|---|---|
| `.card-polished` | 1.25rem | 0.75rem | 1px `--border` | The default content card. Lifts 1px on hover with a `0 4px 24px rgba(6,182,212,0.08)` glow. |
| `.stat-card` | 1.25rem | 0.75rem | 1px `--border` | KPI tiles. Flex-column, `gap: 0.5rem`. No hover effect. |
| `.memory-card` | 1rem | 0.5rem | 1px `--border` | Searchable engram rows. Border goes `--primary` on hover. |

**Never** add a drop shadow to a card at rest. The hover glow on
`.card-polished` is the only sanctioned shadow, and it is primary-tinted,
not neutral.

### 6.2 Buttons

| Class | Fill | Text | Border | Behavior |
|---|---|---|---|---|
| `.btn-primary` | `linear-gradient(135deg, --primary, --primary-dark)` | `#fff` | none | The single affirmative action per view. `opacity: 0.9` on hover. |
| `.btn-secondary` | transparent | `--text-secondary` → `--primary` | 1px `--border` → `--primary` | Default button; secondary actions. |
| `.btn-danger` | transparent → `rgba(239,68,68,0.1)` on hover | `--danger` | 1px `--danger` | Destructive only. Always requires a confirm modal. |
| `.btn-ghost` | transparent → `--bg-elevated` on hover | `--text-secondary` | none | Icon-button chrome; low-emphasis actions. |
| `.btn-sm` | — | — | — | Modifier; `0.3rem 0.75rem` padding, `0.8125rem` font. |

Rules:
- **One primary button per view.** If two appear equally prominent, one is
  mis-classed.
- **Gradients are allowed only on `.btn-primary` and on progress/confidence
  fills.** Nowhere else.
- **`.btn-danger` is never a gradient.** Translucent fill + colored border is
  the destructive grammar across the entire UI (see also badges).

### 6.3 Badges

All badges share `border-radius: 9999px`, `padding: 0.125rem 0.625rem`,
`font-size: 0.75rem`, `font-weight: 600`. Each variant pairs a translucent
fill with a solid text color from the matching token:

| Class | Fill alpha | Text |
|---|---|---|
| `.badge-success` | `rgba(34,197,94,0.15)` | `--success` |
| `.badge-danger` | `rgba(239,68,68,0.15)` | `--danger` |
| `.badge-warning` | `rgba(245,158,11,0.15)` | `--warning` |
| `.badge-info` | `rgba(59,130,246,0.15)` | `--info` |
| `.badge-active` | `rgba(16,185,129,0.15)` | `#10b981` (emerald) |
| `.badge-idle` | `rgba(234,179,8,0.15)` | `--yellow` |
| `.badge-dormant` | `rgba(100,116,139,0.15)` | `--text-muted` |

The `0.15` alpha is the **single sanctioned translucency for status
badges** — do not tune it per instance.

### 6.4 Inputs and forms

`.input-field` is the only text-input style:

```
background: var(--bg-elevated);
border: 1px solid var(--border);
border-radius: 0.5rem;
color: var(--text-primary);
padding: 0.5rem 0.75rem;
height: 2.375rem;          /* fixed height — inputs never grow with content */
font-size: 0.875rem;
font-family: inherit;
outline: none;
transition: border-color 0.15s;
```

Focus is signaled **only** by `border-color: var(--primary)`. No ring, no
shadow. Native `<select>` is styled to match (with `color-scheme: dark` to
force the dropdown arrow to render correctly); options inherit
`--bg-elevated` and `--text-primary`.

`.form-group` is a flex-column with `gap: 0.375rem`; labels are
`0.875rem / 500 / --text-secondary`.

### 6.5 Tabs

`.tab-bar` is a horizontal flex with a shared `1px --border` bottom rule.
Each `.tab-btn` is a borderless tab that picks up the border on activation:

- Inactive: `color: --text-muted`, transparent, no border.
- Hover (inactive): `color: --text-primary`, faint wash
  (`rgba(255,255,255,0.05)` dark / `rgba(0,0,0,0.04)` light).
- Active: `color: --accent`, `font-weight: 600`, `background: --bg-surface`,
  with `border-color: --border` on three sides and a `--bg-surface`
  bottom-border that hides the shared rule — the classic "tab merges into
  the panel" effect.

This is the one place `--accent` (purple) is used for "current selection."
Tabs and the vault picker are the only consumers.

### 6.6 Segmented control

`.seg-btn` with `.seg-active` for the selected segment. Same hover grammar
as tabs: `rgba(255,255,255,0.05)` wash on dark. Use these for tightly
coupled mutually-exclusive choices (e.g. a `List / Grid` toggle) where a
full tab bar is too heavy.

### 6.7 Modals

`.modal-backdrop` is `position: fixed; inset: 0; z-index: 60` with
`rgba(0,0,0,0.75)` and `backdrop-filter: blur(4px)`. The `.modal-box` is
`max-width: 560px`, `.card-polished` styling, `padding: 1.5rem`.

Every destructive action opens a modal from this class; there are no native
`confirm()` dialogs in the codebase.

### 6.8 Toasts

`.notification-toast` is a fixed stack at `bottom: 1.5rem; right: 1.5rem`,
`z-index: 100`. Each `.toast` slides in from the right (`slideIn` keyframe,
0.2s) and recolors its border by type: `.toast.success` (green),
`.toast.error` (red), `.toast.info` (blue). Toasts are the **only** place
a "warning" variant is not used — warnings become modals.

### 6.9 Tooltips

`.tip::after` reads `attr(data-tip)` and renders a 0.6875rem label above the
host, with a deliberate `transition: opacity 0.1s ease 0.15s` — i.e. a
**150ms show delay**, roughly 4× faster than the browser-native tooltip.
Tooltips are dark (`#1e293b` on `#e2e8f0` text) in both themes, capped at
`max-width: 220px`, and `white-space: pre` so contributors can control line
breaks.

### 6.10 Sidebar

The sidebar is the spine of the UI and has the most opinionated styling:

- Collapsed width 56px, expanded 220px. Toggle is a 22px circular button
  that straddles the sidebar/content border (`transform: translateX(-50%)`).
- Logo is 28×28, `border-radius: 5px` (the only non-standard radius).
- Nav items are 18px left-padded, `0.75rem` vertical, with a 20×20 icon
  (`.sidebar-icon`) and a label that fades in (`opacity: 0 → 1`) over 0.2s
  when the sidebar expands.
- Hover: `color → --primary`, `background: rgba(6,182,212,0.06)`.
- Active: same color, `rgba(6,182,212,0.1)` — the canonical 0.06/0.1 ramp.
- Footer holds the vault picker, theme toggle, and version label
  (`0.625rem`, `opacity: 0.5`, only visible when expanded).

### 6.11 Vault chip and picker

Two equivalent vault selectors exist:

- **`.vault-chip`** — a pill next to the page title for inline switching.
  Translucent white fill (`rgba(255,255,255,0.05)`) on dark.
- **`.vault-picker-dropdown` / `.sidebar-vault-panel`** — the command-palette
  modal style. Search input on top, scrollable list (`max-height: 320px`),
  selected item shows a check icon in `--accent`.

Both use `.vault-modal-item.is-active` with `color: --accent` and a
`rgba(6,182,212,0.08)` wash — the only place cyan wash and purple text
appear together.

### 6.12 Progress and confidence meters

Two-bar grammar shared by embeddings and recall scores:

- `.embed-progress-bar` / `.embed-progress-fill` — 6px tall, radius 3px.
- `.confidence-bar` / `.confidence-fill` — 4px tall, radius 2px.

Both fills use the same `linear-gradient(90deg, --primary, --accent)`
(cyan → purple). Width transitions over 0.3–0.4s.

### 6.13 Live indicator

`.ws-dot` is an 8px circle: green when the SSE socket is connected, red
with a 1.5s `pulse` keyframe (`opacity: 1 → 0.3 → 1`) when disconnected.
This pulse is the **only ambient animation** in the UI.

---

## 7. Iconography

- **Source:** Lucide-style glyphs, inlined as SVG. No icon font, no sprite.
- **Canvas:** `viewBox="0 0 24 24"` universally.
- **Stroke:** `stroke="currentColor"`, `fill="none"`.
- **Default stroke-width:** `2`. Heavier (`2.5`) only for small emphatic
  marks (12–14px check icons, vault chevrons).
- **Standard sizes:** sidebar/nav icons are 20×20 (via `.sidebar-icon`);
  inline icons are 12, 14, or 16px. Never 18px — that size is deliberately
  absent from the scale.
- **Color follows context:** icons inherit `color` from their parent, so a
  `.sidebar-item:hover` icon turns cyan automatically via `currentColor`.
- **Line-caps:** `stroke-linecap="round"` and `stroke-linejoin="round"` on
  any icon below 16px to avoid mitered corners at small sizes.

---

## 8. Motion

| Token | Duration | Easing | Property |
|---|---|---|---|
| Hover lift | 0.15s | linear | `transform`, `box-shadow` |
| Border/color | 0.15s | linear | `border-color`, `color`, `background` |
| Sidebar expand | 0.25s | `ease` | `width` |
| Label fade-in | 0.2s | `ease` | `opacity` |
| Tab/tooltip | 0.1s + 0.15s delay | `ease` | `opacity` |
| Toast entry | 0.2s | `ease` | `transform translateX`, `opacity` |
| Progress fill | 0.3–0.4s | `ease` | `width` |
| Disconnect pulse | 1.5s | infinite | `opacity` (keyframe) |

Rules:
- **150ms is the default.** Anything slower must justify itself.
- **No motion on initial render.** Animations are triggered by user or
  server state changes, not by page load.
- **Respect `prefers-reduced-motion`.** The pulse keyframe and the toast
  slide-in are the two candidates for suppression; both should degrade
  gracefully to an instant state change.

---

## 9. Z-index scale

The codebase uses a small, explicit stack. Do not invent intermediate
values:

| Layer | z-index | Owner |
|---|---|---|
| Base | 0 (auto) | Page content. |
| Sidebar | 50 | `.app-sidebar`, `.sidebar-toggle` is 51. |
| Modal | 60 | `.modal-backdrop`. |
| Toasts | 100 | `.notification-toast`. |
| Picker dropdown | 200 | `.vault-picker-dropdown`, `.tip::after`. |

Inline `z-index: 40` and `42` appear in a couple of graph overlays; treat
them as part of the Cytoscape canvas's internal stacking, not part of the
app scale.

---

## 10. Layout grammar

### 10.1 View model

The entire app is one Alpine root (`muninnApp`) with a `currentView` state
variable. Eight views: `dashboard`, `memories`, `graph`, `observability`,
`session`, `settings`, `cluster`, `logs`. Switching is done with
`x-show="currentView === '<name>'"` — there is no router, no URL per view.

### 10.2 Page anatomy

Every view follows the same skeleton:

1. A page-title row, optionally with a `.vault-chip` and action buttons.
2. An optional `.tab-bar` for in-page sections.
3. A responsive grid of `.card-polished` / `.stat-card` blocks.
4. Content (`.memory-card` lists, tables, the Cytoscape `#cy` canvas).

### 10.3 Grid

`.app-main` is capped at `max-width: 1400px`. Card grids are CSS grid (via
Tailwind utilities in the template) and collapse to one column at the
single breakpoint `@media (max-width: 768px)`. There is no tablet-specific
layout — the UI is desktop-first by declaration.

### 10.4 Authentication gate

The `<body>` carries `x-show="isAuthenticated" x-cloak`. Until login
completes, the app shell is hidden; the login form is a separate
`x-show="!isAuthenticated"` block. `[x-cloak]` is defined as
`display: none !important` in `base.css` — the canonical FOUC guard.

---

## 11. Accessibility

- **Color is never the only signal.** Status badges pair color with a text
  label; the live dot pairs color with shape (and the disconnect pulse).
- **Focus visibility** relies on border-color change. For keyboard users
  this is adequate on `.input-field` but weak on buttons — a future
  improvement is a `:focus-visible` ring.
- **Screen-reader text** uses the `.sr-only` utility (absolute, 1×1,
  clipped). Use it for icon-only buttons.
- **Tooltips** are decorative; the `data-tip` attribute is not an
  accessible label. Pair every `.tip` icon button with an `aria-label` or
  `.sr-only` text.
- **`aria-hidden="true"`** is set on decorative SVGs (e.g. the sparkline
  strip in the header).
- **Reduced motion** is not currently handled — see section 8.

---

## 12. Theme switching

- Theme is stored in `localStorage['muninnTheme']` (`'light'` or absent).
- A blocking inline script in `<head>` reads the value and adds the `light`
  class to `documentElement` **before paint** — this is mandatory to avoid
  a dark-to-light flash.
- The toggle lives in the sidebar footer; it mutates both the class and the
  storage entry.
- The `light` class lives on `<html>`, never on `<body>`. Every CSS rule
  that needs a light variant is scoped to `html.light .selector`.

---

## 13. CSS architecture

```
app.css                  ← entry; @imports the three layers below, then @tailwind
├── theme.css            ← :root and html.light custom properties only
├── base.css             ← element resets, body font, scrollbar, [x-cloak]
└── components.css       ← every reusable class (.card-polished, .btn-*, .badge-*, …)
```

Rules:

- **No inline styles in production markup.** The only sanctioned inline
  styles are one-off SVG `width`/`height` and the rare `style="flex-shrink:0"`.
- **No `!important` except for hover overrides** on segmented controls
  (`.seg-btn:hover:not(.seg-active)`) and responsive grid collapse
  (`grid-template-columns: 1fr !important`). Both are documented in
  `components.css`.
- **Tailwind utilities are allowed in markup** for one-off layout tweaks
  (e.g. grid column counts), but every reusable pattern must be promoted to
  a named class in `components.css`.
- **CSS custom properties are the only variable mechanism.** No Sass, no
  CSS-in-JS, no Tailwind theme extension (`theme.extend` is empty in
  `tailwind.config.js`).

---

## 14. Do / Don't

**Do**
- Add new color needs as tokens in `theme.css`, with a dark and (if needed)
  light value.
- Reuse `.card-polished`, `.stat-card`, `.btn-secondary`, `.input-field`,
  and `.badge-*` before inventing new variants.
- Use `--primary` for "interactive," `--accent` for "current," and nothing
  else for emphasis.
- Cap motion at 300ms and tie it to a state change.
- Inline every icon as `viewBox="0 0 24 24"` SVG with `currentColor`.

**Don't**
- Introduce a drop shadow on a resting element.
- Use `--accent` (purple) on a button, link, or hover state.
- Add a `font-size` below `0.625rem` — it is the floor.
- Use `confirm()` or `alert()` — always a `.modal-backdrop` or `.toast`.
- Hard-code a hex value in a component class. Reference a token.
- Add a new `z-index` between 50 and 100. Pick the nearest layer in the
  scale and live within it.

---

## 15. Change control

This guide is regenerated by hand from the MuninnDB source. When the CSS in
`web/static/css/` changes in a way that affects this document:

1. Update the affected section here in the same PR that changes the CSS.
2. Bump the "Last reviewed against" line below.
3. If a new token, class, or z-index layer is added, add a row to the
   relevant table — do not let the tables drift.

**Last reviewed against:** `web/static/css/{theme,base,components,app}.css`
and `web/templates/index.html` at repository `master` (commit
`b9c61193ca191f3bfbbbb90decd6def17f12c775`, 2025-Q1 tree).

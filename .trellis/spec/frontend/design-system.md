# Cinema Design System

> Visual language contract for the Web package: brand palette, theme tokens, motion, and component primitives.

---

## Brand

- **Primary: Aurora Violet** (`brand-*`, `#8b5cf6` / `#7c3aed`) — aligned with the app logo gradient. Used for primary actions, active navigation, focus rings, progress bars, and brand moments.
- **Secondary: Starlight Gold** (`gold-*`, `#f0b34e`) — ratings, featured/premium accents only. Never a primary action color.
- **Tertiary: Pulse Cyan** (`sage-*`, `#22d3ee`) — informational accents (matches the logo's cyan dot).

## Themes

- **Dark is the default theme** (`useThemeMode.readStoredTheme` falls back to `'dark'`). Cinema dark: near-black violet-tinted surfaces (`--app-bg: #07070c`), glass panels, ambient glow via `body::before`.
- Light theme ("Editorial Paper") is a first-class variant: cool white `#f7f7fa`, same violet brand.
- All theming flows through CSS variables in `web/src/index.css` (`--app-*`). Never hardcode hex colors in components; use the variables or `brand-*`/`gold-*`/`sage-*` Tailwind scales.
- Legacy light utility classes (`bg-white`, `bg-gray-*`, `text-gray-*`, …) are remapped in dark mode by the `:root[data-theme='dark']` override block in `index.css`. Keep new overrides there when adding hardcoded light classes.

## Motion

- Springs over easings for micro-interactions (`stiffness 300, damping 26` on cards); page transitions use `cubic-bezier(0.21, 0.47, 0.32, 0.98)` (`ease-smooth`).
- Posters: hover = scale ~1.03–1.06 + violet glow (`shadow-poster-hover`) + gradient overlay; rating badges are glass (`bg-black/55 backdrop-blur`) with gold stars.
- Ambient motion (login backdrop) uses slow framer-motion loops, disabled under `prefers-reduced-motion`.

## Component primitives (index.css `@layer components`)

- `.btn-primary` = violet gradient + glow shadow + hover sheen sweep (`::after` highlight); `.btn-danger` = red gradient variant for destructive confirms; `.btn-outline`, `.btn-ghost` for secondary actions; `.icon-btn` = 36px square ghost button (dialog close, toolbar slots).
- `.card` / `.glass-panel`, `.input-field`, `.badge-brand|sage|gold|neutral`, `.data-table`, `.skeleton`, `.text-gradient-brand`.
- **Selects**: use `Select` (`web/src/components/Select.tsx`) instead of native `<select>`. Keep options as `<option>` children and pass the existing string value to `onChange`; the shared component owns the themed Portal menu, keyboard navigation, disabled/required behavior, and focus return.
- **Modals**: every dialog uses `ModalShell` (`web/src/components/ModalShell.tsx`) — framer-motion spring entrance (scale 0.94 → 1, `stiffness 420, damping 32`, respects `prefers-reduced-motion`), theme-aware `.modal-backdrop` (`--app-overlay` + 10px blur), `.modal-panel`, `.modal-header`, `.modal-footer`, `.modal-icon` (`--danger`/`--gold` variants), body scroll lock. Pass `onClose` to enable backdrop-click + Escape close; omit it for dirty forms. Set `zIndex` for stacked dialogs (confirm 100, password/PIN 110).
- Toasts (react-hot-toast in `main.tsx`) read `--app-glass`/`--app-text` vars; success icon violet, error red. Don't add per-call `className` overrides.
- Backward-compatible aliases (`surface-card`, `field-input`, `amber-btn--solid`, …) map to the new primitives — keep them working.

## Quality Check

- [ ] New UI uses CSS variables / brand scales, not hardcoded hex.
- [ ] Product UI does not introduce native `<select>` elements; dropdowns reuse the shared `Select`.
- [ ] Dark and light themes both verified with screenshots.
- [ ] Hover/focus states carry the violet glow language.

## Series Detail: Watching First

- Whole-series detail management exposes metadata editing, track probing and record deletion, not smart scraping or filesystem organizing. Movie and Episode detail menus also omit filesystem organizing; use dedicated organizing tools instead. Optional shared menu actions are rendered only when the corresponding callback is supplied.
- Series owns the main hero. Season context, episode selection and selected Episode share a content surface. Episode details use a landscape still and technical column beside metadata/actions on desktop, with metadata before tracks in mobile reading order. A single season needs no selector; season counts refer to playable logical episodes, not file versions. Do not invent Season synopsis from Series/Episode fields.
- Place Series/Episode playback actions before secondary metadata using `MediaDetailMetadata.actions`. Synopsis, file/library facts and read-only tracks are directly visible, without disclosure panels; movie metadata remains expanded. Season and version selectors remain interactive choices, not content disclosures.
- Use `MediaDetailTracks readOnlyTracks` for Series episode details: version selection changes the playback file, while video/audio/subtitle tracks are explicitly read-only text. Do not present informational tracks as playback settings.
- Episode metadata omits ratings and Douban association (including standalone Episode pages); keep the Episode TMDb link. Read-only tracks reuse movie information-row shells/icons; subtitles use the compact movie chooser to inspect one track at a time, not to configure playback. Desktop artwork rows must not grow with the adjacent synopsis; keep the technical rows directly below artwork.
- Show a small Season-owned cover/title alongside the season controls. Load from a visible file through `/media/:id/season`; key asynchronous results to that file and reject mismatched season numbers. Missing artwork uses a placeholder, not Series/Episode artwork; failed reads offer retry.
- Keep episode-card playback explicit. Selecting an already visible card must not unnecessarily reposition the horizontal strip.
- Verify primary-action reachability with long synopsis content, not only absence of overflow. Check directly visible metadata, selected-file links and refresh restoration. Run `node web/scripts/check-series-presentation.mjs` alongside the selection regression check.

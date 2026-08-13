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
- **Modals**: every dialog uses `ModalShell` (`web/src/components/ModalShell.tsx`) — framer-motion spring entrance (scale 0.94 → 1, `stiffness 420, damping 32`, respects `prefers-reduced-motion`), theme-aware `.modal-backdrop` (`--app-overlay` + 10px blur), `.modal-panel`, `.modal-header`, `.modal-footer`, `.modal-icon` (`--danger`/`--gold` variants), body scroll lock. Pass `onClose` to enable backdrop-click + Escape close; omit it for dirty forms. Set `zIndex` for stacked dialogs (confirm 100, password/PIN 110).
- Toasts (react-hot-toast in `main.tsx`) read `--app-glass`/`--app-text` vars; success icon violet, error red. Don't add per-call `className` overrides.
- Backward-compatible aliases (`surface-card`, `field-input`, `amber-btn--solid`, …) map to the new primitives — keep them working.

## Quality Check

- [ ] New UI uses CSS variables / brand scales, not hardcoded hex.
- [ ] Dark and light themes both verified with screenshots.
- [ ] Hover/focus states carry the violet glow language.

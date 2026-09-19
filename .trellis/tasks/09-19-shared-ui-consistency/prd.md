# 统一前端子标签与公共组件样式

## Goal

核对系统设置、发现及其他页面的重复样式，将同用途组件统一到共享实现并验证交互兼容性

## Requirements

- R1: Explain why system-settings category tabs differ from discovery and other page tabs, using current source evidence.
- R2: Give equivalent page tabs one reusable visual contract, preserving route/query state, permissions, settings drafts, and native link/button semantics.
- R3: Survey other repeated UI primitives and unify proven equivalent patterns using the existing design system; preserve intentional business-specific layouts.
- R4: Keep the change in the Web presentation layer, without new dependencies or backend behavior changes.
- R5: Existing input/button compatibility aliases must match their canonical styles in dark and light themes, including focus and hover.

## Acceptance Criteria

- [x] System settings, discovery and other equivalent tabs share active, inactive, hover, focus and responsive styling in light/dark themes.
- [x] Existing URL navigation, settings drafts/save and discovery lazy loading continue to pass their runnable regression checks.
- [x] Other component inconsistencies and intentional exceptions are accounted for with source evidence.
- [x] Frontend lint, build, relevant browser checks and an independent diff review pass.
- [x] No debug services or temporary artifacts remain after verification.

## Background

- `web/src/pages/SettingsPage.tsx:87` uses an individually styled rounded navigation container.
- `web/src/pages/DiscoverHubPage.tsx:21` uses independently styled underline buttons.
- Equivalent navigation appears in Me, Libraries, Tasks, Download Space and Auto Organize settings.
- Seven tables repeat base styling rather than using `.data-table`; two already use it. Table cells may contain forms/actions, so shared styling must not impose truncation.
- `web/src/index.css:491` applies canonical styles to aliases, but dark-mode selectors below match only canonical class names.
- The user asks to unify styles and reuse equivalent components, rather than redesign business flows.

## Out of Scope

- Backend changes, new component dependencies, blanket redesign of intentionally different controls, and unrelated existing work.

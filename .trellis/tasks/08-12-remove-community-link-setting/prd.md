# Remove obsolete community link setting

## Goal

Remove the login-page community link footer and its obsolete visibility
setting end to end so the administrator is not offered a control for UI that
the product no longer exposes.

## Background

- `ui.hide_community_links_for_users` currently appears as a general-settings
  toggle.
- The key exists solely to control the login-page footer containing the GitHub
  repository, author page, and Telegram group links.
- `/api/public/ui-config` exists solely to expose that key to the footer.

## Requirements

- Remove the login-page community link footer rather than leaving it visible or
  conditionally hidden.
- Remove the `ui.hide_community_links_for_users` general-settings item.
- Remove the dedicated Web API client/type and backend public UI configuration
  endpoint because they have no remaining consumer or field.
- Remove tests that assert the retired endpoint and replace them only if needed
  to protect a remaining behavior.
- Preserve all other login behavior, settings, routes, and authenticated page
  layout.
- Do not add a database migration for historical rows containing the retired
  key; an inert orphan setting is harmless and schema cleanup would add risk
  without changing runtime behavior.

## Out of Scope

- Redesigning the login page.
- Removing unrelated public endpoints or general settings.
- Cleaning unrelated historical setting rows.

## Acceptance Criteria

- [x] The login page no longer renders the GitHub repository, author page, or
      Telegram group footer.
- [x] The general settings page no longer lists the community-link visibility
      toggle or its description.
- [x] The production source contains no reference to
      `ui.hide_community_links_for_users`, `hide_community_links_for_users`, or
      `/public/ui-config`; tests may name the retired route only to assert that
      it is no longer registered.
- [x] Frontend lint and production build pass.
- [x] Focused backend handler tests and `git diff --check` pass.

## Open Questions

None.

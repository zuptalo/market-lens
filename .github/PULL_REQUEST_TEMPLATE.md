<!--
Thanks for contributing to Market Lens. Keep the summary focused on behavior.
See CONTRIBUTING.md for the full workflow.

Title this pull request using an allowed conventional form, for example:
`feat(market-data): ingest daily Nordic price bars`. The validated title becomes the
squash commit title and determines the automatic semantic-version bump.
-->

## What & why

<!-- What does this change do, and why? Link any issue. -->

## Checklist

- [ ] Branch is named `NNN-lowercase-kebab` for the governing specification.
- [ ] PR title uses an allowed conventional type and accurately signals the SemVer bump.
- [ ] A reviewed specification defines the changed production behavior.
- [ ] Each production-code change began with a focused automated test that was observed
      failing for the expected reason before the implementation was written.
- [ ] The implementation makes those tests pass without weakening or skipping them.
- [ ] `make verify` passes.
- [ ] `npm run test:e2e` run if this affects user-facing flows (or N/A).
- [ ] Every database transformation uses a new ordered migration; no manual database
      manipulation or production-access step is required.
- [ ] Documentation and the relevant specification match the delivered behavior.
- [ ] Squash is appropriate as the single permanent commit for this change.

## Operational impact

<!-- Note migrations, configuration, background jobs, compatibility, and rollout risks. -->

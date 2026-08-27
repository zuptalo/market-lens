<!--
Thanks for contributing to Market Lens. Keep the summary focused on behavior.
See CONTRIBUTING.md for the full workflow.
-->

## What & why

<!-- What does this change do, and why? Link any issue. -->

## Checklist

- [ ] A reviewed specification defines the changed production behavior.
- [ ] Each production-code change began with a focused automated test that was observed
      failing for the expected reason before the implementation was written.
- [ ] The implementation makes those tests pass without weakening or skipping them.
- [ ] `make verify` passes.
- [ ] `npm run test:e2e` run if this affects user-facing flows (or N/A).
- [ ] Every database transformation uses a new ordered migration; no manual database
      manipulation or production-access step is required.
- [ ] Documentation and the relevant specification match the delivered behavior.

## Operational impact

<!-- Note migrations, configuration, background jobs, compatibility, and rollout risks. -->

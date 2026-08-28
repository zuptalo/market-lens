# Feature Specification: Release Versioning and Protected Delivery

**Feature Branch**: `003-release-versioning`

**Created**: 2026-08-28

**Status**: in-review
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: Require specification work to happen on relevant feature branches and reach
`main` only through pull requests. Keep main history linear through squash merging,
derive an automatic semantic version when a pull request is shipped, publish a versioned
container image and release after required verification succeeds, and display the exact
running version in the application without exposing build or credential details.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Ship only reviewed, verified pull requests (Priority: P1)

As the project maintainer, I can only add work to `main` through a pull request from a
descriptive feature branch, and the pull request cannot merge until required automated
checks pass, so the release history stays reviewable and protected from accidental
direct changes.

**Why this priority**: Automated versioning and publishing are trustworthy only when the
source branch and merge boundary are controlled.

**Independent Test**: Attempt a direct update and a force update to `main`, then open a
conforming pull request and verify that only the pull-request path becomes mergeable
after all required checks succeed.

**Acceptance Scenarios**:

1. **Given** a contributor attempts to push directly to `main`, **When** repository rules
   evaluate the update, **Then** the update is rejected, including for administrators.
2. **Given** a pull request whose source branch or title does not follow the documented
   convention, **When** its policy check runs, **Then** merging remains blocked with an
   actionable explanation.
3. **Given** a conforming pull request with a failing required check or unresolved review
   conversation, **When** merge is attempted, **Then** merging remains blocked.
4. **Given** a conforming pull request whose required checks pass, **When** the maintainer
   merges it, **Then** GitHub performs a squash merge, deletes the feature branch, and
   leaves one linear commit on `main`.

---

### User Story 2 - Produce a deterministic release automatically (Priority: P1)

As the operator, I receive a unique semantic version, immutable release record, and
versioned application image after a pull request is merged successfully, without manually
editing version files or guessing which source revision was deployed.

**Why this priority**: A version must identify one verified source state and one
deployable artifact or it cannot support reliable operations.

**Independent Test**: Evaluate representative conforming pull-request titles against a
known prior release, then simulate a successful merge and verify the expected next
version and complete image-tag/release set are produced exactly once.

**Acceptance Scenarios**:

1. **Given** the latest release is `0.1.0`, **When** a `feat:` pull request is merged,
   **Then** the resulting release version is `0.2.0`.
2. **Given** a latest release, **When** a conforming non-feature, non-breaking pull
   request is merged, **Then** the patch component increments by one.
3. **Given** a pull-request title explicitly marks a breaking change, **When** it is
   merged, **Then** the major component increments and the other components reset.
4. **Given** post-merge verification succeeds, **When** release automation completes,
   **Then** it creates one immutable version tag and GitHub release and publishes the
   application image under the full version, compatible major/minor aliases, commit
   identity, and `latest`.
5. **Given** required verification or publishing fails, **When** the release workflow
   stops, **Then** it does not falsely report a completed GitHub release and exposes a
   retryable failure without logging secrets.

---

### User Story 3 - Identify the running application version (Priority: P2)

As a user or operator, I can see the exact version running from every application page
and confirm the same identity through system health information, so deployments can be
verified without inspecting a container manually.

**Why this priority**: Visible identity closes the loop between source release, image,
deployment, and user-facing instance.

**Independent Test**: Start a build with a known version, open the application at each
required viewport, and verify that the visible version and health response match exactly.

**Acceptance Scenarios**:

1. **Given** a production build identified as `0.2.0`, **When** any application route is
   displayed, **Then** `v0.2.0` is visible in the application shell without opening a
   diagnostic tool.
2. **Given** the same running build, **When** its health information is requested, **Then**
   the returned version is exactly `0.2.0`.
3. **Given** a local build without a release version, **When** the application is opened,
   **Then** it visibly identifies itself as a development build rather than claiming a
   released version.

### Edge Cases

- No prior semantic-version tag exists when release automation is first introduced.
- A pull-request title contains an allowed type and optional scope but unusual spacing,
  mixed case, or an invalid/missing description.
- A breaking marker appears on a feature or fix title before version 1.0.0.
- Two main-branch workflow events could try to allocate the next version concurrently.
- A release workflow is rerun after the image was pushed but before the release was
  created.
- A tag or release for the calculated version already exists.
- A pull request contains only documentation or CI changes but still ships a new
  application artifact.
- The container build uses a tag checkout rather than a moving branch checkout.
- The application shell is viewed at 320 CSS pixels or with long development metadata.
- The frontend and backend build identities are accidentally different.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: All planned feature work MUST use a descriptive branch named with its
  three-digit specification identifier followed by a lowercase kebab-case short name.
- **FR-002**: `main` MUST reject direct pushes, force pushes, and deletion for every
  contributor, including repository administrators.
- **FR-003**: Changes MUST reach `main` only through pull requests with all required
  automated checks successful and all review conversations resolved.
- **FR-004**: The repository MUST allow squash merge as the only pull-request merge
  method and MUST delete a merged feature branch automatically.
- **FR-005**: A pull-request title MUST follow an allowed conventional form with a
  non-empty description; invalid titles MUST block merging with an actionable message.
- **FR-006**: The squash commit title MUST preserve the validated pull-request title so
  release behavior and main history remain understandable.
- **FR-007**: Every successful pull-request merge to `main` MUST produce exactly one next
  semantic version after post-merge verification succeeds.
- **FR-008**: Version selection MUST be deterministic: a breaking marker increments
  major, `feat` increments minor, and every other allowed change type increments patch.
- **FR-009**: The initial release baseline MUST be recorded as `0.1.0`; later versions
  MUST derive from the greatest existing semantic release tag rather than a manually
  edited source version.
- **FR-010**: Release automation MUST serialize version allocation so concurrent events
  cannot publish the same version.
- **FR-011**: A completed release MUST create an immutable `vMAJOR.MINOR.PATCH` source tag
  and GitHub release for the verified main commit.
- **FR-012**: A completed release MUST publish one application image digest addressable
  by `MAJOR.MINOR.PATCH`, `MAJOR.MINOR`, `MAJOR`, the source commit, and `latest` tags.
- **FR-013**: The build MUST inject the same normalized version into the backend and
  frontend without embedding credentials or depending on a manual source edit.
- **FR-014**: Release automation MUST NOT mark a GitHub release complete when required
  verification or image publication fails and MUST be safe to retry.
- **FR-015**: The running version MUST remain available through health information and
  MUST be visibly present in the global application shell on every route.
- **FR-016**: Released versions MUST display as `vMAJOR.MINOR.PATCH`; unreleased local
  builds MUST display an explicit development identity.
- **FR-017**: Version display MUST use text, remain readable in system/light/dark themes,
  and not depend on hover, color, or a diagnostic-only interaction.
- **FR-018**: Repository contribution and release documentation MUST explain branch/title
  conventions, squash delivery, semantic bump rules, required checks, image tags,
  recovery behavior, and how to confirm a deployment version.
- **FR-019**: Versioning and release automation MUST use only repository-provided tokens
  and standard package/release permissions; it MUST NOT require a new committed secret.

### Test-First Proof *(mandatory)*

- **Initial failing test**: A release-version script test covering feature, patch,
  breaking, malformed-title, and no-prior-tag cases.
- **Expected red reason**: The repository has no deterministic release-version policy
  implementation, so valid titles cannot produce the required next semantic version;
  missing test setup or unrelated shell failure does not qualify.
- **Green evidence**: Release-policy tests, frontend version tests, API tests, workflow
  syntax/static contract tests, `make verify`, relevant Playwright, Docker build, and
  Compose validation all pass.
- **Database migration proof**: N/A; this feature changes no persistent application data.

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: The compact version text remains visible in the global
  shell without clipping navigation/theme controls or causing horizontal page scrolling.
  The 360x800 scenario verifies visibility; the 320x800 scenario verifies tolerance for
  development metadata.
- **Tablet (768-1023 CSS px)**: At 768x1024 the version remains visible without reducing
  primary navigation/action usability, and orientation changes preserve shell state.
- **Desktop (1024+ CSS px)**: At 1440x900 the version appears as quiet secondary metadata
  in the application shell and does not compete with page content.
- **Input and accessibility**: Version identity is ordinary readable text exposed to
  assistive technology, uses sufficient theme contrast, needs no pointer interaction,
  remains legible at browser zoom, and conveys no meaning through color alone.

### Key Entities *(include if feature involves data)*

- **Release Version**: A semantic major/minor/patch identity allocated from prior release
  tags and the validated merged change type.
- **Release Artifact**: One verified application image digest with immutable full-version
  and commit tags plus moving compatibility and latest aliases.
- **Build Identity**: The normalized version compiled into both application layers and
  returned/displayed at runtime.
- **Repository Rule**: The enforced main-branch, pull-request, merge-method, and required-
  check policy controlling delivery.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of attempted direct, forced, or deletion updates to `main` are rejected.
- **SC-002**: 100% of merged changes reach `main` as one squash commit after every
  required check passes and every conversation is resolved.
- **SC-003**: A test matrix of feature, fix, breaking, documentation, malformed, and
  bootstrap inputs produces the expected next version or rejection in every case.
- **SC-004**: Within 20 minutes of a verified merge, one release and one image digest are
  available under all five required tag forms.
- **SC-005**: The version displayed in the shell, returned by health information, attached
  to the release, and embedded in the image metadata matches in 100% of release checks.
- **SC-006**: The running version is visible without navigation or developer tools at
  360x800, 768x1024, and 1440x900, with no accidental horizontal scrolling at 320 pixels.
- **SC-007**: A deliberately failed verification/publish scenario creates no completed
  GitHub release and exposes no credential value in logs.

## Assumptions

- The repository remains public and uses GitHub-hosted pull requests, Actions, Releases,
  and Container Registry.
- One maintainer must be able to merge their own pull request; required approvals are
  therefore zero while pull requests, checks, and resolved conversations remain mandatory.
- Allowed title types initially include `feat`, `fix`, `perf`, `refactor`, `docs`, `test`,
  `build`, `ci`, `chore`, and `revert`, with optional scope and `!` breaking marker.
- Every merge produces a deployable artifact and therefore at least a patch release;
  there is no `release:none` path.
- `v0.1.0` identifies the verified foundation baseline that predates this automation.
- The existing `latest`-based optional k3s deployment continues following successful
  releases, while immutable tags support diagnosis and future pinning.

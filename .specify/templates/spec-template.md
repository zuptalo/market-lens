# Feature Specification: [FEATURE NAME]

**Feature Branch**: `[###-feature-name]`

**Created**: [DATE]

**Status**: planned
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: User description: "$ARGUMENTS"

## User Scenarios & Testing *(mandatory)*

<!--
  IMPORTANT: User stories should be PRIORITIZED as user journeys ordered by importance.
  Each user story/journey must be INDEPENDENTLY TESTABLE - meaning if you implement just ONE of them,
  you should still have a viable MVP (Minimum Viable Product) that delivers value.

  Assign priorities (P1, P2, P3, etc.) to each story, where P1 is the most critical.
  Think of each story as a standalone slice of functionality that can be:
  - Developed independently
  - Tested independently
  - Deployed independently
  - Demonstrated to users independently
-->

### User Story 1 - [Brief Title] (Priority: P1)

[Describe this user journey in plain language]

**Why this priority**: [Explain the value and why it has this priority level]

**Independent Test**: [Describe how this can be tested independently - e.g., "Can be fully tested by [specific action] and delivers [specific value]"]

**Acceptance Scenarios**:

1. **Given** [initial state], **When** [action], **Then** [expected outcome]
2. **Given** [initial state], **When** [action], **Then** [expected outcome]

---

### User Story 2 - [Brief Title] (Priority: P2)

[Describe this user journey in plain language]

**Why this priority**: [Explain the value and why it has this priority level]

**Independent Test**: [Describe how this can be tested independently]

**Acceptance Scenarios**:

1. **Given** [initial state], **When** [action], **Then** [expected outcome]

---

### User Story 3 - [Brief Title] (Priority: P3)

[Describe this user journey in plain language]

**Why this priority**: [Explain the value and why it has this priority level]

**Independent Test**: [Describe how this can be tested independently]

**Acceptance Scenarios**:

1. **Given** [initial state], **When** [action], **Then** [expected outcome]

---

[Add more user stories as needed, each with an assigned priority]

### Edge Cases

<!--
  ACTION REQUIRED: The content in this section represents placeholders.
  Fill them out with the right edge cases.
-->

- What happens when [boundary condition]?
- How does system handle [error scenario]?

## Requirements *(mandatory)*

<!--
  ACTION REQUIRED: The content in this section represents placeholders.
  Fill them out with the right functional requirements.
-->

### Functional Requirements

- **FR-001**: System MUST [specific capability, e.g., "allow users to create accounts"]
- **FR-002**: System MUST [specific capability, e.g., "validate email addresses"]
- **FR-003**: Users MUST be able to [key interaction, e.g., "reset their password"]
- **FR-004**: System MUST [data requirement, e.g., "persist user preferences"]
- **FR-005**: System MUST [behavior, e.g., "log all security events"]

*Example of marking unclear requirements:*

- **FR-006**: System MUST authenticate users via [NEEDS CLARIFICATION: auth method not specified - email/password, SSO, OAuth?]
- **FR-007**: System MUST retain user data for [NEEDS CLARIFICATION: retention period not specified]

### Test-First Proof *(mandatory)*

- **Initial failing test**: [Name the automated test that will express the first missing
  behavior and why it must fail before implementation]
- **Expected red reason**: [Describe the behavioral assertion that fails; setup or
  compilation failures do not qualify]
- **Green evidence**: [Identify the suite that must pass after implementation]
- **Database migration proof**: [If persistent data changes, identify the migration test
  proving a clean database upgrades without manual steps, or state N/A]

### Responsive UI Behavior *(mandatory for user-facing features; otherwise state N/A)*

- **Mobile (320-767 CSS px)**: [Describe layout, navigation, actions, dense-data
  treatment, touch behavior, and the 360x800 automated acceptance scenario]
- **Tablet (768-1023 CSS px)**: [Describe layout and the 768x1024 automated acceptance
  scenario]
- **Desktop (1024+ CSS px)**: [Describe layout and the 1440x900 automated acceptance
  scenario]
- **Input and accessibility**: [Describe keyboard, touch, non-hover, orientation, zoom,
  and state-preservation expectations]

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

- **Snapshot and events**: [Define initial REST state and the versioned SSE event types
  emitted for every client-visible committed change]
- **Reliability**: [Define authorization scope, event IDs, ordering, Last-Event-ID
  resumption, duplicate handling, bounded buffering, and stale/offline behavior]
- **Test evidence**: [Define automated reconnect, missed-event replay, duplicate,
  slow-consumer, and cross-user event-isolation scenarios]

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

- **Bootstrap and invitations**: [Define first-owner creation, setup closure, verified
  email invitation, expiry, single use, and roles]
- **Ownership and authorization**: [Define shared versus private records and the backend
  query/service checks that prevent cross-user access]
- **Security evidence**: [Define session/recovery/revocation and cross-user tests]

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

- **Installability**: [Define mobile/tablet/desktop Chrome and Edge installation and
  offline/degraded behavior]
- **Consent and delivery**: [Define granular opt-in email/Web Push preferences, quiet or
  frequency controls, minimal payloads, per-device revocation, and unsubscribe]
- **Test evidence**: [Define denied/expired permissions, offline delivery, removed
  devices, unavailable providers, and privacy tests]

### Key Entities *(include if feature involves data)*

- **[Entity 1]**: [What it represents, key attributes without implementation]
- **[Entity 2]**: [What it represents, relationships to other entities]

## Success Criteria *(mandatory)*

<!--
  ACTION REQUIRED: Define measurable success criteria.
  These must be technology-agnostic and measurable.
-->

### Measurable Outcomes

- **SC-001**: [Measurable metric, e.g., "Users can complete account creation in under 2 minutes"]
- **SC-002**: [Measurable metric, e.g., "System handles 1000 concurrent users without degradation"]
- **SC-003**: [User satisfaction metric, e.g., "90% of users successfully complete primary task on first attempt"]
- **SC-004**: [Business metric, e.g., "Reduce support tickets related to [X] by 50%"]

## Assumptions

<!--
  ACTION REQUIRED: The content in this section represents placeholders.
  Fill them out with the right assumptions based on reasonable defaults
  chosen when the feature description did not specify certain details.
-->

- [Assumption about target users, e.g., "Users have stable internet connectivity"]
- [Assumption about scope boundaries, e.g., "Mobile support is out of scope for v1"]
- [Assumption about data/environment, e.g., "Existing authentication system will be reused"]
- [Dependency on existing system/service, e.g., "Requires access to the existing user profile API"]

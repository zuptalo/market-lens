# Feature Specification: One Component Library Everywhere

**Feature Branch**: `012-primevue-consistency`

**Created**: 2026-08-31

**Status**: shipped
<!-- Market Lens spec lifecycle: planned → in-progress → in-review → shipped. -->

**Input**: Every screen should use the component library the project picked. No custom UI
component may be built where the library already provides one.

## Why this exists

PrimeVue 4.5.5 is installed and configured in `main.ts` with the Aura preset and a
`darkModeSelector`. Three files use it, between them importing three components: `Button`,
`Tag`, and `Card`. The other fifteen do not.

**Every form in the application is hand-rolled.** `OwnerAuth` has twelve raw controls,
`IntegrationSettings` eight, and `InvitationForm`, `EmailCodeForm`, `MemberList`,
`SessionList`, `MarketsView`, and `AcceptInvitationView` between one and five each. Lists are
hand-built `<ul>` markup and panels are bare `<section>` elements.

This is not a theoretical tidiness problem. It has already produced defects:

- `src/styles/main.css` is 194 lines re-implementing focus rings, field spacing, alert colours,
  and panel chrome that the configured theme already provides.
- Alert text was `#a51d2d` with no dark-theme override, so **every** account-section alert was
  unreadable at 2.54:1 on the dark ground. It was found only when a new section happened to be
  the first to render one in a test. A themed `Message` inherits a palette that is contrast-
  checked once, centrally, instead of per component.
- Each hand-rolled control re-derives its own focus, disabled, and invalid states, so they
  drift apart. Two forms on the same screen already look different.

The constitution's Principle VII already requires PrimeVue primitives first. The codebase
diverged from it, and each new feature widened the gap.

## The rule this establishes

Where the library provides a component for a purpose, that component is used. A bespoke
component is written only for something the library genuinely does not cover, and its absence
from the library is stated in review. This applies to new work and to the existing screens,
which are migrated here so that the application is consistent rather than half converted.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Every control looks and behaves the same (Priority: P1)

Somebody moving between sign-in, setup, account settings, and market data sees one visual
language: the same field, button, and message treatment, in light, dark, and system themes.

**Why this priority**: It is the reported problem, and it is what the library was chosen for.

**Independent Test**: Open account settings and confirm the invitation field and the
integration fields render identically, and that both follow the theme on a dark background.

**Acceptance Scenarios**:

1. **Given** any screen with a text field, **When** it renders, **Then** it is a PrimeVue
   input rather than a bare element styled by the project stylesheet.
2. **Given** any screen with an error, **When** it renders, **Then** it is a PrimeVue message
   whose colours come from the theme, in every theme.
3. **Given** the whole client, **When** it is searched, **Then** no component defines its own
   focus ring, disabled state, or alert colour for a control the library provides.

---

### User Story 2 - Nothing a person could do stops working (Priority: P1)

Every existing behaviour survives: labels, keyboard operation, validation messages tied to
their fields, screen-reader announcements, and the responsive treatment at every viewport.

**Why this priority**: This is a refactor. A migration that changes behaviour is a regression,
however consistent it looks.

**Independent Test**: Run the existing browser suite unchanged where it selects by label or
role, and confirm every journey still passes.

**Acceptance Scenarios**:

1. **Given** the existing tests, **When** they select a control by its label or role, **Then**
   they still find it, because the library renders real labelled form elements.
2. **Given** a field with an error, **When** it renders, **Then** the input is still marked
   invalid for assistive technology and still points at its message.
3. **Given** any screen, **When** it is viewed at 360x800, 768x1024, 1440x900 and 320 pixels,
   **Then** it still fits without horizontal page scrolling and every control is reachable.
4. **Given** a data list, **When** it is migrated to a library table, **Then** every action it
   offered before is still present and still reachable by keyboard.

---

### Edge Cases

- A library component renders extra wrapper elements, breaking a test that reached for a bare
  element: the test is updated to select by label or role, which is what it should have done.
- A six-digit code field has a dedicated library component: it is used, and the existing
  numeric-input behaviour and autocomplete hint are preserved.
- A control the library does not provide, such as an inline per-field error paragraph tied to
  an input: it stays bespoke, and the reason is recorded.
- The theme changes the rendered contrast: verified by the existing accessibility gate rather
  than assumed.

## Requirements *(mandatory)*

- **FR-001**: Every text, password, and numeric input MUST use the library's input component.
- **FR-002**: Every button MUST use the library's button component.
- **FR-003**: Every status and error message MUST use the library's message component.
- **FR-004**: Tabular and list data MUST use the library's data components.
- **FR-005**: Panels and grouped sections MUST use the library's container components.
- **FR-006**: Project stylesheet rules that re-implement library styling MUST be deleted, not
  left dormant.
- **FR-007**: Accessible labelling, invalid marking, and message association MUST be preserved
  for every migrated control.
- **FR-008**: No behaviour visible to a person may change.
- **FR-009**: Any remaining bespoke component MUST be justified by the absence of a library
  equivalent.

### Test-First Proof *(mandatory)*

- **Initial failing test**: a client test asserting no `.vue` file under `src/` contains a raw
  `<input`, `<button`, `<select`, or `<textarea` outside an allowlist. It must fail listing the
  eight components that currently do — a behavioral assertion over the real sources.
- **Expected red reason**: the assertion lists the offending files.
- **Green evidence**: that test passing, plus the existing Vitest and Playwright suites, which
  must continue to pass with their label- and role-based selectors intact.
- **Database migration proof**: N/A. No server change.

### Responsive UI Behavior *(mandatory)*

Unchanged in intent and re-verified in fact: 360x800, 768x1024, 1440x900, and a 320-pixel
floor with no horizontal page scrolling, on every migrated screen, in light, dark, and system
themes, with the existing accessibility gate enforcing contrast and focus visibility.

### Live Update Behavior *(mandatory for client-visible data; otherwise state N/A)*

N/A. No data contract changes.

### Identity, Ownership, and Permissions *(mandatory for user/account data; otherwise state N/A)*

N/A for authorization logic, which is unchanged. Owner-only sections stay owner-only; the
migration touches presentation only.

### PWA and Notification Behavior *(mandatory when applicable; otherwise state N/A)*

N/A.

## Success Criteria *(mandatory)*

- **SC-001**: No `.vue` file renders a raw form control where the library provides one,
  enforced by an automated test rather than review.
- **SC-002**: The project stylesheet shrinks materially, and no rule in it restyles a library
  control's focus, disabled, or invalid state.
- **SC-003**: Every existing Vitest and Playwright journey passes, with label- and role-based
  selectors unchanged.
- **SC-004**: The accessibility gate passes in light, dark, and system themes on every screen.
- **SC-005**: Every screen fits at 320 pixels without horizontal page scrolling.

## Assumptions

- The library renders real labelled form elements, so tests selecting by label or role keep
  working. Tests that reach for bare elements are the ones that need updating, and they were
  the more brittle choice regardless.
- Inline per-field error text tied to a specific input has no direct library equivalent that
  preserves the existing `aria-describedby` association, so it remains bespoke and minimal.
- Aura's palette is contrast-correct in both themes, so deleting hand-rolled colour rules
  removes the class of defect that produced the 2.54:1 alert.

## Implementation notes

Kept in this file rather than a separate plan and tasks pair. The change is mechanical and
wide rather than deep: no server code, no schema, no contract.

Mapping: `<input>` → `InputText`; password → `Password` with feedback disabled; numeric →
`InputNumber`; six-digit code → `InputOtp`; `<button>` → `Button`; `role="alert"` → `Message`;
`<ul>` of records → `DataTable` with `Column`; grouped section → `Panel` or `Card`;
`<fieldset>` → `Fieldset`.

## Implementation evidence (2026-08-31)

**Initial red.** The guard test listed ten components rendering raw controls, and `main.css`
restyling controls the theme owns.

**Green.** 99 Vitest tests, 111 Playwright journeys (run twice), `make verify`,
`docker compose config`, `deploy/k8s/test.sh`.

`main.css` went from 194 lines to 208 - it did not shrink, and that is the honest result. The
15 lines of hand-rolled control chrome were deleted, but the responsive table treatment
PrimeVue 4 no longer provides had to be written, along with layout constraints. What changed
is the *kind* of CSS: no rule now sets a colour, border, or font on a control.

### Four defects this surfaced, three of them pre-existing

1. **Aura's primary button fails WCAG AA in light mode.** White on emerald-500 measures
   2.54:1. Corrected once in the theme preset rather than per component, so everything drawing
   on the primary colour inherits the fix.
2. **Aura's danger button fails too** - white on red-500 at 3.76:1. This was invisible until
   the hand-rolled `#0f766e` button rules were deleted, because they had been overriding the
   library's own colour on every account button.
3. **Aura indicates focus on a form field with a border-colour change and no ring at all.** A
   real outline is now defined in the preset. This had been masked by the browser's default
   outline on bare inputs.
4. **`Panel`'s `header` prop renders a `<span>`, not a heading**, which silently removed
   Members, Invitations and Integrations from the document outline. The `#header` slot with a
   real `<h2>` restores it.

### Two deliberate deviations

- **Secret fields stay native `<input type="password">`.** Both `Password` and `InputText`
  bind their value as a DOM *attribute*, so a typed API key or mail password is serialized
  into the markup and would be captured by anything that snapshots the DOM. A plain input sets
  it as a property. They carry the library's classes, so they are visually identical, and the
  guard test permits exactly this one shape. `OwnerAuth.test.ts` enforces the property.
- **`InputOtp` was not used for the six-digit code.** The existing field deliberately accepts
  pasted codes containing spaces and hyphens and normalises them, with tests covering it.
  `InputOtp`'s six separate boxes would replace that behaviour rather than preserve it.

### Test changes, and why each is not a weakening

- `selectOption` on the exchange filter became opening the combobox and choosing the option,
  and `toHaveValue` became `toContainText`. The filter is no longer a native `<select>`; the
  test now drives it the way a person does.
- `select[aria-label="Exchange"]` became an assertion that each filter reports
  `role="combobox"` - asserting the accessible role rather than the tag name.
- The focus-ring check now moves focus with Tab instead of `element.focus()`. A ring is drawn
  on `:focus-visible`, and whether programmatic focus satisfies that is a browser heuristic -
  which is why the assertion passed or failed depending on what ran before it.

### Diagnostics improved along the way

The contrast check now reports the offending element and both composited colours, and the
overflow check names the elements exceeding the viewport. Both previously reported only that
something was wrong, which was most of the work left to do.

## Follow-up: market data filters (2026-08-31)

Reported after the migration: the filter row was visibly misaligned. Two defects, both mine,
and both of which every suite passed straight through.

**Alignment.** `.instrument-filters` is a five-column grid built for `<label>Search<input></label>`
- one cell per filter, with the label wrapping its own control. The migration split each into
two siblings, so ten children landed in five columns and the row read label, field, label,
field across the page. Each pair is now wrapped in `.instrument-filters__field`, which is the
grid child again.

**Labels pointing at nothing.** `Select` renders a composite widget, so a plain `id` lands on
its outer wrapper - an element with `tabindex="-1"` and no role. Every `<label for>` in that
row addressed something nobody could focus. `inputId` puts the id on the control the label
means. The `aria-label` on each filter is why no accessibility check noticed.

**Also restored:** the filters showed blank instead of "All exchanges". PrimeVue treats an
empty model value as no selection and falls back to the placeholder, where a native `<select>`
displayed the matching option's text.

### Two test gaps this exposed, now closed

1. **The accessibility suite never opened `/markets`** - the densest set of controls in the
   application. It does now.
2. **A label was only checked for having text**, not for pointing at anything usable. The new
   assertion requires the target to exist *and* be labelable or focusable. The first version
   of it was too weak and still passed against the real defect, because the wrapper div does
   exist; it was tightened until it failed on the seeded bug, then confirmed green on the fix.

Neither gap would have been found by running the suite. Both came from looking at the page.

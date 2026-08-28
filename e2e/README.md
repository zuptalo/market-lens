# End-to-end tests

The baseline Playwright suite verifies the production-built application shell at
representative mobile (360x800), tablet (768x1024), and desktop (1440x900) viewports.
User-facing feature specifications must add responsive scenarios here, including a
320px overflow/clipping check when the feature introduces new layout behavior.

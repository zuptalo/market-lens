# Feature Specification: k3s deployment

**Feature Branch**: `001-k3s-deployment`

**Created**: 2026-08-28

**Status**: in-progress

**Input**: Deploy Market Lens to the existing k3s cluster using the proven Ring
PostgreSQL, Keel, and Traefik conventions. Serve it at
`market-lens.zuptalo.com`, redirect public HTTP to HTTPS, automatically roll out new public GHCR `latest` image
digests within approximately two minutes, and issue/renew a Let's Encrypt
certificate without committing secrets.

## User Scenarios & Testing

### User Story 1 - Reproducible cluster deployment (Priority: P1)

An operator can deploy Market Lens and PostgreSQL with one idempotent installer,
without manually editing the database or committing credentials.

**Independent Test**: Render every manifest with test configuration, validate it
with `kubectl --dry-run=client`, and verify the required secret references.

**Acceptance Scenarios**:

1. **Given** an existing k3s cluster, **When** the installer runs, **Then** it
   creates or updates the namespace, PostgreSQL, application, routing, and updater
   resources.
2. **Given** the generated Kubernetes Secret already exists, **When** the installer
   runs again, **Then** it preserves the existing database password and connection
   string.

### User Story 2 - Automatic tested-image rollout (Priority: P1)

An operator receives a rolling Market Lens deployment after a new
`ghcr.io/zuptalo/market-lens:latest` digest is published.

**Independent Test**: Verify the Deployment uses `latest`, `Always`, and the Ring
Keel poll annotations with an `@every 2m` schedule.

**Acceptance Scenarios**:

1. **Given** Keel is running, **When** the public `latest` digest changes, **Then**
   Keel initiates a rolling update within approximately two minutes.

### User Story 3 - Automatic HTTPS (Priority: P1)

An operator configures a hostname and ACME email and receives an automatically
renewed Let's Encrypt certificate on the cluster's public port 443.

**Independent Test**: Verify the rendered Traefik configuration enables a
persistent ACME TLS challenge resolver and the Ingress selects that resolver.

**Acceptance Scenarios**:

1. **Given** the hostname resolves to the cluster and TCP 443 reaches Traefik,
   **When** the manifests are applied, **Then** Traefik obtains and serves a Let's
   Encrypt certificate for that hostname.
2. **Given** TCP 80 reaches Traefik, **When** a client requests the configured host
   over HTTP, **Then** Traefik redirects the request to HTTPS.

### Edge Cases

- A rerun must not rotate an existing database password.
- The installer must reject a missing hostname or ACME email.
- PostgreSQL data and Traefik ACME account state must survive pod replacement.
- Application readiness must remain false while PostgreSQL is unavailable.

## Requirements

### Functional Requirements

- **FR-001**: The deployment MUST use a dedicated `market-lens` namespace.
- **FR-002**: PostgreSQL 18 MUST use a retained 10 Gi `local-path` volume.
- **FR-003**: Database credentials MUST exist only in a generated Kubernetes Secret.
- **FR-004**: The application MUST run the public
  `ghcr.io/zuptalo/market-lens:latest` image and expose only its HTTP service inside
  the cluster.
- **FR-005**: Liveness and readiness probes MUST use `/api/v1/health` and
  `/api/v1/ready` respectively.
- **FR-006**: Keel MUST poll the mutable image tag every two minutes and force a
  rolling update when its digest changes.
- **FR-007**: Traefik MUST terminate HTTPS using a Let's Encrypt TLS-ALPN-01
  resolver whose state is persistent.
- **FR-008**: Hostname and ACME email MUST be installer inputs, not source edits.
- **FR-009**: The installer MUST be idempotent and MUST NOT apply manual SQL or
  directly manipulate PostgreSQL.
- **FR-010**: Plain HTTP for the configured hostname MUST redirect to HTTPS without
  intercepting unrelated hosts handled by the upstream proxy.

### Test-First Proof

- **Initial failing test**: `deploy/k8s/test.sh`
- **Expected red reason**: the required deployment manifests and installer do not
  exist before implementation.
- **Green evidence**: the manifest contract test and Kubernetes client-side dry run
  pass after implementation, followed by validation against the target k3s cluster.
- **Database migration proof**: No schema change. The application continues to run
  its embedded ordered migrations at startup; deployment performs no manual SQL.

## Success Criteria

### Measurable Outcomes

- **SC-001**: Every rendered manifest passes Kubernetes client-side validation.
- **SC-002**: A new `latest` digest is detected on the configured two-minute poll.
- **SC-003**: `https://market-lens.zuptalo.com/api/v1/health` returns HTTP 200 after
  deployment and certificate issuance.
- **SC-004**: Re-running the installer changes neither the existing database secret
  nor persistent data.

## Assumptions

- k3s uses its bundled Traefik and `local-path` storage, as observed in the target
  cluster.
- Public TCP 443 is forwarded to the k3s Traefik service and the configured DNS name
  resolves to that public endpoint.
- The GHCR package remains public, so neither the application nor Keel needs registry
  credentials.
- The requested public hostname is `market-lens.zuptalo.com`; Kubernetes resource
  names remain `market-lens` to match the project and image.

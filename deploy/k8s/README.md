# Market Lens on k3s

These manifests deploy the production Market Lens image and PostgreSQL to the
existing k3s cluster, configure Keel to poll GHCR every two minutes, redirect HTTP
to HTTPS, and configure k3s Traefik to obtain and renew a Let's Encrypt certificate
using TLS-ALPN-01 on public port 443.

```text
Internet :443
     |
     v
k3s Traefik -- Let's Encrypt TLS termination
     |
     v
market-lens:8080 --> market-lens-postgres:5432
     ^
     |
Keel polls ghcr.io/zuptalo/market-lens:latest every 2m
```

## Prerequisites

- k3s with bundled Traefik and the `local-path` storage class
- TCP 443 forwarded to the k3s node; TCP 80 is recommended for the HTTP redirect
- an A/AAAA record resolving the application hostname to that endpoint
- `kubectl`, `openssl`, and `envsubst` (`gettext`) on the installer machine
- an email address for Let's Encrypt expiration and account notices

The installer modifies the cluster-wide k3s Traefik `HelmChartConfig` to add a
resolver named `letsencrypt` and replaces Traefik's ephemeral `/data` volume with a
128 Mi persistent volume. This preserves the ACME account and certificates across
Traefik restarts. It does not install cert-manager because this cluster does not have
it and TLS-ALPN-01 works with the already-public port 443.

## Upstream Traefik forwarding

When another Traefik instance fronts k3s, keep HTTPS as raw TCP passthrough so the
k3s Traefik receives the original SNI and ACME ALPN challenge. Plain HTTP has no SNI,
so it must use an HTTP router if different hostnames have different destinations.

In the current NAS setup, the internet router forwards public `80` to the outer
Traefik's `web` entrypoint on NAS port `880`, and public `443` to its `websecure`
entrypoint on NAS port `4443`. Those listener ports belong only in the outer
Traefik's static command (`:880` and `:4443`). The dynamic backend targets remain
the k3s Traefik service at `10.0.1.4:80` and `10.0.1.4:443`.

For the current cluster at `10.0.1.4`, the relevant outer-proxy configuration is:

```yaml
tcp:
  routers:
    market-lens:
      entryPoints: [websecure]
      rule: "HostSNI(`market-lens.zuptalo.com`)"
      priority: 100
      tls: { passthrough: true }
      service: k3s

    passthrough-rest:
      entryPoints: [websecure]
      rule: "HostSNI(`*`)"
      priority: 1
      tls: { passthrough: true }
      service: dsm-https

  services:
    k3s:
      loadBalancer:
        servers:
          - address: "10.0.1.4:443"
    dsm-https:
      loadBalancer:
        servers:
          - address: "127.0.0.1:443"

http:
  routers:
    market-lens-http:
      entryPoints: [web]
      rule: "Host(`market-lens.zuptalo.com`)"
      priority: 100
      service: k3s-http

    dsm-http-fallback:
      entryPoints: [web]
      rule: "PathPrefix(`/`)"
      priority: 1
      service: dsm-http

  services:
    k3s-http:
      loadBalancer:
        servers:
          - url: "http://10.0.1.4:80"
    dsm-http:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:80"
```

Remove the old `tcp.routers.http-80` catch-all. TCP routers are evaluated before HTTP
routers, so leaving `HostSNI(`*`)` on the `web` entrypoint would continue to capture
every port-80 connection before the host-aware HTTP router can see it. Keep the Ring
and other existing `websecure` TCP routers alongside the `market-lens` router.

## Install

The requested hostname is the default:

```sh
cd deploy/k8s
ACME_EMAIL=you@example.com ./install.sh
```

Override it when deploying elsewhere:

```sh
APP_HOST=market-lens.example.com ACME_EMAIL=you@example.com ./install.sh
```

The installer generates `POSTGRES_PASSWORD`, `DATABASE_URL`, and an
independent external-credential encryption key/version once in the
`market-lens-secrets` Kubernetes Secret. Re-running it preserves existing values and
adds a missing complete credential key pair; it never rotates an existing key. An
incomplete key/version pair stops the upgrade for operator repair. No credential is
stored in the repository, and no manual SQL is executed; the Go application applies its
embedded migrations on startup.

It does **not** generate `AUTH_SECRET`. The application provisions its own signing key on
first start and keeps it in the database, so it travels with a backup. A deployment that
already holds an `AUTH_SECRET` keeps it: the manifest still injects it (optionally), the
application prefers it, and the installer never removes it, because removing it would sign
every user out. `EXTERNAL_CREDENTIAL_KEY` is the one value you must retain alongside a
database backup - it encrypts provider credentials held inside that database and is
deliberately never stored there.

## Verify

```sh
kubectl -n market-lens get pods,service,ingress,pvc
kubectl -n market-lens rollout status deployment/market-lens
kubectl -n keel logs deployment/keel --tail=100
curl https://market-lens.zuptalo.com/api/v1/health
```

Inspect the certificate served by Traefik:

```sh
echo | openssl s_client \
  -connect market-lens.zuptalo.com:443 \
  -servername market-lens.zuptalo.com 2>/dev/null \
  | openssl x509 -noout -issuer -subject -dates
```

## Files

| File | Purpose |
|---|---|
| `00-namespace.yaml` | Dedicated application namespace |
| `05-traefik-acme.yaml` | Persistent cluster-wide Traefik ACME resolver |
| `10-postgres.yaml` | PostgreSQL 18 StatefulSet, Service, and retained PVC |
| `20-market-lens.yaml` | Application ConfigMap, Service, and Keel-annotated Deployment |
| `30-ingress.yaml` | HTTP redirect, HTTPS routing, and certificate resolver selection |
| `40-keel.yaml` | Idempotent cluster-wide Keel installation |
| `install.sh` | Secret generation, manifest rendering, application, and rollout checks |
| `test.sh` | Static contract checks and optional Kubernetes client-side validation |

## Backup and recovery

Back up both the PostgreSQL PVC/data and the `market-lens-secrets` Secret. Deleting a
Deployment does not remove the database PVC. Do not repair or change production data
with manual SQL; all persistent transformations belong in ordered application
migrations.

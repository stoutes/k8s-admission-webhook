# k8s-admission-webhook

A production-ready Kubernetes admission webhook server in Go with:

- **Mutating webhook** — injects an Envoy sidecar into opted-in pods
- **Validating webhook** — rejects Deployments missing CPU/memory limits
- **cert-manager** integration — automatic TLS provisioning and `caBundle` rotation
- **RBAC** — least-privilege ServiceAccount, Role, and ClusterRole

## Project layout

```
.
├── cmd/webhook/          # main entrypoint (flags, mux setup)
├── internal/
│   ├── admission/        # shared decode/encode/patch helpers
│   ├── mutate/           # sidecar injection handler + tests
│   └── validate/         # resource limits handler + tests
├── deploy/
│   ├── cert-manager/     # ClusterIssuer, CA Certificate, Issuer, TLS Certificate
│   ├── rbac/             # ServiceAccount, Role, ClusterRole + bindings
│   └── webhook/          # Deployment, Service, ConfigMap, WebhookConfigurations
├── Dockerfile
└── Makefile
```

## Prerequisites

- Kubernetes 1.25+
- cert-manager v1.13+ (`make cert-manager-install`)
- Docker (or buildah/podman)

## Quick start

```bash
# 1. Install cert-manager
make cert-manager-install

# 2. Build and push the image
make docker-build docker-push IMAGE=yourregistry/admission-webhook TAG=v0.1.0

# 3. Update the image reference in deploy/webhook/deployment.yaml, then:
make deploy

# 4. Smoke test
make verify
```

## How it works

### Sidecar injection (mutating)

Intercepts `CREATE` on `Pod` in namespaces labeled `webhook-injection: enabled`.

| Condition | Behaviour |
|---|---|
| annotation `sidecar-injector.webhook-system/inject: "false"` | skipped |
| annotation `sidecar-injector.webhook-system/injected: "true"` | skipped (idempotency) |
| otherwise | injects envoy sidecar + shared ConfigMap volume |

### Resource limits enforcement (validating)

Intercepts `CREATE` and `UPDATE` on `Deployment` in all namespaces except the
system ones. Denies any deployment where a container is missing a CPU or memory limit.

### TLS and caBundle

cert-manager provisions a self-signed CA, issues the webhook server's TLS cert,
and automatically patches the `caBundle` field in both `WebhookConfiguration`
objects on every rotation via the `cert-manager.io/inject-ca-from` annotation.

## Running tests

```bash
make test
```

## Opting namespaces in/out

```bash
# opt a namespace into sidecar injection
kubectl label namespace my-namespace webhook-injection=enabled

# opt a single pod out at the object level
kubectl run my-pod --image=nginx \
  --overrides='{"metadata":{"annotations":{"sidecar-injector.webhook-system/inject":"false"}}}'
```

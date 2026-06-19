# NetBird Kubernetes Operator.

The NetBird Kubernetes Operator brings NetBird network access into the Kubernetes
API. NetBird API objects — networks, resources, DNS zones and records, groups,
setup keys, reverse-proxy services — are mirrored 1:1 as custom resources, and
the Gateway API is the translation layer that turns in-cluster Services into
NetBird exposure. Everything is declarative and reconciled, so NetBird access is
managed the same way as the rest of your cluster.

See [`docs/architecture.md`](docs/architecture.md) for the design.

## Features

**NetBird-mirror CRDs**

* Thin, 1:1 mirrors of NetBird API objects — **`Network`, `NetworkResource`,
  `DNSZone`, `DNSRecord`, `ReverseProxyService`, `Group`, `SetupKey`**. Each
  reconciles its spec straight to the NetBird Management API and records the
  returned id in its status.
* **Automatic secret management** — setup keys are stored as Kubernetes secrets.
* Works with NetBird Cloud or self-hosted.

**Service exposure (Gateway API)**

* A **`Gateway`** of the `netbird` class links to a `Network`: it deploys the
  routing-peer pods and joins them to the network, and creates a `DNSZone` from
  its listener hostname.
* An **`HTTPRoute`** or **`TCPRoute`** makes its backend Services reachable in
  the network — one `NetworkResource` (the ClusterIP, routed via the peers) and
  one `DNSRecord` (`<svc>-<ns>.<zone>`) per backend and IP family, so dualstack
  Services are reachable over both.
* A **`ReverseProxyService`** publishes a route through NetBird's reverse proxy.
  It is **admin-authored** — creating one is the explicit decision to expose a
  route — and references the route to pick up its backends:
  * `proxyCluster` — the NetBird reverse-proxy cluster that serves it (e.g. `gate.example.com`)
  * `upstream` — `hostname` (default; proxy resolves the Service FQDN, IPv4/IPv6 transparent) or `ip` (ClusterIP)
  * `private` + `accessGroups`, `crowdsecMode`, `accessRestrictions`, `passHostHeader`, `rewriteRedirects`

## How it works

1. A **`Network`** mirrors a NetBird network. A **`Gateway`** of the `netbird`
   class links to it via its single listener
   (`protocol: gateway.netbird.io/Network`, `name:` the Network), deploys the
   routing-peer pods, and creates a **`DNSZone`** from the listener `hostname`.
2. Attaching an **`HTTPRoute`** or **`TCPRoute`** to that Gateway makes its
   backend Services reachable: the operator creates a `NetworkResource` and a
   `DNSRecord` per backend ClusterIP family. (Route kind doesn't change this —
   both produce reachability; the difference is whether you expose them.)
3. To expose a route publicly, create a **`ReverseProxyService`** referencing it.
   The operator resolves the `proxyCluster`, builds the reverse-proxy service's
   `cluster` targets from the route's backends, and the proxy reaches them via
   the routing peers (resolving the FQDN through the distributed zone).

A single **`GatewayClass`** — `netbird` — is provided by the operator.

## Quick start

Install the Gateway API CRDs, create the API-key secret, and install the
operator (enable `gatewayAPI`):

```shell
kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.0/experimental-install.yaml
kubectl create namespace netbird
kubectl -n netbird create secret generic netbird-mgmt-api-key --from-literal NB_API_KEY=${NETBIRD_API_KEY}
helm upgrade --install --create-namespace -n netbird netbird-operator \
  oci://ghcr.io/netbirdio/helm-charts/netbird-operator --set gatewayAPI.enabled=true
```

Create a network and a Gateway that fronts it:

```yaml
apiVersion: netbird.io/v1alpha1
kind: Network
metadata: { name: kube, namespace: netbird }
spec:
  name: kube
---
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata: { name: public, namespace: netbird }
spec:
  gatewayClassName: netbird
  listeners:
    - name: kube                              # -> the Network above
      protocol: gateway.netbird.io/Network
      port: 1
      hostname: kube.example.com              # -> the DNS zone
```

Make a Service reachable and expose it through the reverse proxy:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata: { name: app, namespace: default }
spec:
  parentRefs: [{ name: public, namespace: netbird }]
  hostnames: ["app.example.com"]
  rules: [{ backendRefs: [{ name: app, port: 80 }] }]
---
apiVersion: netbird.io/v1alpha1
kind: ReverseProxyService
metadata: { name: app, namespace: default }
spec:
  routeRef: { kind: HTTPRoute, name: app }    # picks up the route's backends
  proxyCluster: gate.example.com              # your NetBird reverse-proxy cluster
  upstream: hostname                          # or "ip"
  crowdsecMode: observe
```

A full walkthrough (including a private `TCPRoute`) is in
[`examples/gateway-api`](examples/gateway-api/README.md). See the
[NetBird Kubernetes docs](https://docs.netbird.io/manage/integrations/kubernetes)
for management-side setup.

## API

| Kind | API Version | Purpose |
|------|-------------|---------|
| [Network](docs/api-reference.md#network) | `netbird.io/v1alpha1` | A NetBird network |
| [NetworkResource](docs/api-reference.md#networkresource) | `netbird.io/v1alpha1` | One address routed into a network |
| [DNSZone](docs/api-reference.md#dnszone) | `netbird.io/v1alpha1` | A NetBird managed DNS zone (and its distribution) |
| [DNSRecord](docs/api-reference.md#dnsrecord) | `netbird.io/v1alpha1` | A record in a DNSZone |
| [ReverseProxyService](docs/api-reference.md#reverseproxyservice) | `netbird.io/v1alpha1` | Expose a route through the NetBird reverse proxy |
| [Group](docs/api-reference.md#group) | `netbird.io/v1alpha1` | A NetBird group |
| [SetupKey](docs/api-reference.md#setupkey) | `netbird.io/v1alpha1` | A NetBird setup key |
| [SidecarProfile](docs/api-reference.md#sidecarprofile) | `netbird.io/v1alpha1` | Sidecar peer injection profile |
| [ClusterProxy](docs/api-reference.md#clusterproxy) | `netbird.io/v1alpha1` | Cluster-API proxy |

Service exposure also consumes the upstream Gateway API kinds `Gateway`,
`HTTPRoute` and `TCPRoute` (`gateway.networking.k8s.io`). Full field reference:
[`docs/api-reference.md`](docs/api-reference.md).

## Configuration

The operator is configured with command-line flags (see `--help` for the full
list). The most useful ones:

| Flag | Default | Purpose |
|------|---------|---------|
| `--log-level` | `info` | Log verbosity: `debug`, `info`, `warn`, `error`, or a non-negative integer for higher debug verbosity (`2` = `V(2)`). |
| `--log-format` | `json` | Log output: `json` (structured, ISO8601 timestamps) or `console` (human-readable, for local runs). |
| `--gateway-api-enabled` | `false` | Reconcile Gateway API resources (service exposure). |
| `--netbird-management-url` | `https://api.netbird.io` | NetBird Management API URL (set for self-hosted). |
| `--netbird-client-image` | (built-in) | Image for the routing-peer pods a Gateway deploys. |
| `--metrics-bind-address` | `0` (off) | `:8080` for HTTP or `:8443` for HTTPS metrics. |

Routine per-reconcile messages are logged at debug, so at `info` the operator is
quiet unless something notable happens; raise `--log-level=debug` to see the
reconcile trace.

When installed with the Helm chart these are set through values — logging via
`operator.logging.level` / `operator.logging.format`, Gateway API via
`gatewayAPI.enabled`, and the Management URL via `managementURL`.

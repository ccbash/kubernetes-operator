# NetBird Kubernetes Operator

The NetBird Kubernetes Operator automates the provisioning of NetBird network access for services running in your cluster. It extends the Kubernetes API with CRDs, letting you manage NetBird peers, networks, routers, groups, and Gateway-API-based service exposure declaratively, the same way you manage the rest of your infrastructure.

## Features

* **Declarative peer & network management** — define NetBird peers, networks, routers, resources, groups and setup keys as Kubernetes resources; the operator handles provisioning and lifecycle.
* **Automatic secret management** — setup keys and credentials are stored and rotated as Kubernetes secrets.
* **Gateway API integration** — expose Services over NetBird with standard Gateway API objects:
  * an `HTTPRoute` attached to a `netbird-public` Gateway publishes a Service through the NetBird **reverse proxy** (L7, public hostname);
  * a `TCPRoute` attached to a `netbird-private` Gateway exposes a Service as a **private** network resource (L4, reachable only by mesh peers);
  * path-based HTTP routes are honoured — one proxy target per backend, carrying the rule's path prefix.
* **Per-route configuration via `NBServicePolicy`** — a GEP-713 direct-attached policy that configures the reverse-proxy service for the `HTTPRoute`(s) it targets, so the settings aren't reset on every reconcile: `private` + `accessGroups`, `crowdsecMode` (`off`/`observe`/`enforce`), IP/geo `accessRestrictions` (allowed/blocked CIDRs and ISO-3166 countries), `passHostHeader`/`rewriteRedirects`, and `routingMode`.
* **Selectable routing mode** (`routingMode`, per service, default `ip`):
  * `ip` — a host resource at the Service ClusterIP with a host proxy target. DNS-independent; IPv4.
  * `domain` — a domain resource at the Service FQDN with a domain proxy target, plus A/AAAA records. Dualstack, resolved via NetBird DNS.
* **Dualstack DNS** — per-service A and AAAA records are published from the Service's `ClusterIPs`, reconciled against the live zone so they aren't churned.
* **Service-CIDR routing** — `NetworkRouter.spec.serviceCIDRs` routes the cluster's Service CIDRs into the NetBird network as subnet resources, so ClusterIPs are reachable through the routing peers; `resourceGroups` assigns the network's resources to NetBird groups so access policies can target them.
* **Namespace-scoped or cluster-wide** — deploy per-namespace for multi-tenant clusters or cluster-wide for full coverage.
* **Works with any NetBird deployment** — NetBird Cloud or self-hosted.

## Getting Started

For full setup instructions, see the [Getting Started](https://docs.netbird.io/manage/integrations/kubernetes) documentation. The Gateway API integration is walked through in [`examples/gateway-api`](examples/gateway-api/README.md).

Once your API-key secret is configured, install the operator with Helm:

```shell
helm upgrade --install --create-namespace -n netbird netbird-operator oci://ghcr.io/netbirdio/helm-charts/netbird-operator
```

## API

| Kind | API Version |
|------|-------------|
| [Group](docs/api-reference.md#group) | `netbird.io/v1alpha1` |
| [NetworkResource](docs/api-reference.md#networkresource) | `netbird.io/v1alpha1` |
| [NetworkRouter](docs/api-reference.md#networkrouter) | `netbird.io/v1alpha1` |
| [NBServicePolicy](docs/api-reference.md#nbservicepolicy) | `netbird.io/v1alpha1` |
| [SetupKey](docs/api-reference.md#setupkey) | `netbird.io/v1alpha1` |
| [SidecarProfile](docs/api-reference.md#sidecarprofile) | `netbird.io/v1alpha1` |
| [ClusterProxy](docs/api-reference.md#clusterproxy) | `netbird.io/v1alpha1` |

Service exposure also consumes the upstream Gateway API kinds `HTTPRoute` and `TCPRoute` (`gateway.networking.k8s.io`).

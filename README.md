# kubectl-can

[![ci](https://github.com/moveeeax/kubectl-can/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/kubectl-can/actions/workflows/ci.yml)
[![go](https://img.shields.io/badge/go-1.26%2B-00ADD8)](https://go.dev)
[![license](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

> Instantly see what your current Kubernetes context can and cannot do.

`kubectl auth can-i` answers one question at a time. When you've just assumed a role
or switched contexts and want the whole picture — what can this ServiceAccount
actually do here? — you end up running it in a loop. `kubectl-can` runs that loop for
you: it probes a matrix of verbs against common resources with
`SelfSubjectAccessReview` and prints the result grouped by resource, allowed and
denied side by side.

```console
$ kubectl can --namespace prod --resource pods,secrets,deployments
deployments              ✔ get  ✔ list  ✔ watch  ✔ create  ✔ update  ✔ patch  ✗ delete  ✗ deletecollection
pods                     ✔ get  ✔ list  ✔ watch  ✗ create  ✗ update  ✗ patch  ✗ delete  ✗ deletecollection
secrets                  ✗ get  ✗ list  ✗ watch  ✗ create  ✗ update  ✗ patch  ✗ delete  ✗ deletecollection
```

Without `--resource` it probes all 19 default resources (pods, services, configmaps,
secrets, persistentvolumeclaims, serviceaccounts, events, namespaces, nodes,
deployments, statefulsets, daemonsets, replicasets, jobs, cronjobs, ingresses,
networkpolicies, roles, rolebindings) with all 8 default verbs (get, list, watch,
create, update, patch, delete, deletecollection). Resources are printed in
alphabetical order.

The fixed list misses CRDs, and that is where many escalation paths live: a
ServiceAccount that can patch `applications.argoproj.io` or
`certificates.cert-manager.io` looks harmless in the default view.
`--all-resources` probes every resource the API server serves instead:

```console
$ kubectl can --all-resources --group argoproj.io,cert-manager.io -n argocd
applications.argoproj.io     ✔ get  ✔ list  ✔ watch  ✗ create  ✔ update  ✔ patch  ✗ delete  ✗ deletecollection
appprojects.argoproj.io      ✔ get  ✔ list  ✔ watch  ✗ create  ✗ update  ✗ patch  ✗ delete  ✗ deletecollection
certificates.cert-manager.io ✔ get  ✔ list  ✔ watch  ✗ create  ✗ update  ✗ patch  ✗ delete  ✗ deletecollection
...
```

To ask the reverse question, who in the cluster can do something, use `who`:

```console
$ kubectl can who get secrets -n prod --show-escalation
KIND            SUBJECT         BINDING                           ROLE                       ESCALATION
Group           system:masters  ClusterRoleBinding/cluster-admin  ClusterRole/cluster-admin  bind,escalate,impersonate,read-secrets
ServiceAccount  prod/ci         RoleBinding/prod/ci-secrets       ClusterRole/secret-reader  read-secrets
User            alice           RoleBinding/prod/ci-secrets       ClusterRole/secret-reader  read-secrets
```

## What it does

- Probes verbs × resources via `SelfSubjectAccessReview` — the same check `kubectl auth can-i` uses.
- Groups results by resource with ✔ / ✗ markers, colorized.
- Scopes to a namespace, and supports `--as` user / `--as-group` impersonation.
- Filters with `--verb`, `--resource` and `--group`; `--json` for automation.
- `--all-resources` probes every served resource, CRDs included, found through API discovery.
- `who VERB RESOURCE` lists every user, group and ServiceAccount that RBAC grants it, with the bindings that grant it, as a table, JSON or CSV.

## Install

```bash
go install github.com/moveeeax/kubectl-can@latest
# invoked as either `kubectl-can` or, on your PATH, `kubectl can`
```

Prebuilt archives for linux/darwin (amd64, arm64) and windows (amd64) are attached
to each [GitHub release](https://github.com/moveeeax/kubectl-can/releases) together
with `checksums.txt`, starting from the first release cut after the release workflow
was added. Unpack the archive and put `kubectl-can` on your `PATH`.

### krew

The plugin is not in the krew index yet, so `kubectl krew install can` does not work.
The [krew manifest](examples/can.yaml) is filled in for v0.2.0, so you can install from it directly:

```sh
kubectl krew install --manifest-url=https://raw.githubusercontent.com/moveeeax/kubectl-can/main/examples/can.yaml
```

Release binaries for linux, darwin and windows are on the [releases page](https://github.com/moveeeax/kubectl-can/releases).

## Usage

| Flag | Description |
|------|-------------|
| `--kubeconfig` | Path to a kubeconfig file. Without it the standard loading rules apply: `$KUBECONFIG`, then `~/.kube/config`. The current context of that file is used |
| `-n`, `--namespace` | Namespace to check (empty = cluster scope) |
| `--as` | Impersonate a user |
| `--as-group` | Impersonate a group (repeatable) |
| `--verb` | Only probe these verbs (comma-separated) |
| `--resource` | Only probe these resources, by name (`pods`) or `name.group` (`certificates.cert-manager.io`), comma-separated |
| `--group` | Only probe resources in these API groups (comma-separated); `core` selects the core group |
| `--all-resources` | Probe every resource the API server serves (discovery), CRDs included, instead of the 19 defaults |
| `--json` | Emit JSON |
| `--no-color` | Disable colored output |

`--kubeconfig`, `--namespace`, `--as` and `--as-group` also apply to `who`.

### --all-resources

The resource list comes from discovery (`ServerPreferredResources`), so each API
group is probed at its preferred version and every served resource, CRDs included,
is in it. Subresources such as `pods/exec` are not listed. Each resource is probed
only with the verbs the server says it supports, so a create-only API like
`tokenreviews` gets one probe, not eight. With `--namespace` only namespaced
resources are probed, since a namespaced check of a cluster-scoped resource does
not mean much. In this mode resources outside the core group are printed
kubectl-style as `name.group`. If some API groups fail discovery (an aggregated API
server that is down, say) the rest are still probed and a warning goes to stderr.

A cluster with many CRDs means a few hundred `SelfSubjectAccessReview` calls, so
the default stays the 19 built-in resources. Narrow it with `--group` or
`--resource`.

### who

```bash
kubectl can who VERB RESOURCE[.GROUP][/SUBRESOURCE] [--namespace NS] [--resource-name NAME] [--show-escalation] [-o text|json|csv]
```

| Flag | Description |
|------|-------------|
| `-n`, `--namespace` | Namespace of the access to check. Without it only cluster-wide grants (ClusterRoleBindings) count, which is what the authorizer does for a cluster-scope request |
| `--resource-name` | Check access to one object. Rules with `resourceNames` only match a named request, so without this flag they are not counted |
| `--show-escalation` | Add an ESCALATION column listing which of `bind`, `escalate` (on roles/clusterroles), `impersonate` (users, groups, serviceaccounts) and `read-secrets` (get/list/watch secrets) each subject also holds in the same scope |
| `-o`, `--output` | `text` (default), `json`, or `csv` with one row per subject and binding, for access reviews |

`who` reads ClusterRoles and ClusterRoleBindings, plus the namespace's Roles and
RoleBindings with `--namespace`, so the caller needs `list` on those. It expands
aggregated ClusterRoles from their label selectors and matches rules the way the
RBAC authorizer does: `*` verbs, groups and resources, `resourceNames`,
`pods/exec` and `*/exec` subresource rules. A resource without a group is resolved
through the default list, then discovery; a name served by several groups has to be
qualified (`events.events.k8s.io`).

`who` sees RBAC only. Access granted by webhook or Node authorizers does not show
up here, while the default mode, which asks the API server, does see it.
ServiceAccounts and users that get access through a group binding (for example
`system:serviceaccounts` or `system:authenticated`) are listed as that group.

## How it works

For every verb×resource pair it issues a `SelfSubjectAccessReview` with the
resource's API group filled in (so `deployments` is probed under `apps`, `ingresses`
under `networking.k8s.io`, and so on), then groups the answers by resource. Because it
asks the API server the same question `kubectl auth can-i` does, the answers reflect
the *real* authorizer — RBAC, webhooks, node restrictions, all of it — not a
client-side guess. The review call sits behind a `Reviewer` interface, so the probe
matrix, filtering, grouping, and rendering are unit-tested with no cluster.

`who` cannot ask the API server "who can", since there is no such API, so it reads
the RBAC objects and evaluates them client-side with the same matching rules as the
RBAC authorizer. That logic is in `internal/whocan` and is tested against fixture
objects.

## Development

```bash
make build   # go build -o kubectl-can .
make test    # go test -race ./...
```

## License

MIT — see [LICENSE](LICENSE).

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

## What it does

- Probes verbs × resources via `SelfSubjectAccessReview` — the same check `kubectl auth can-i` uses.
- Groups results by resource with ✔ / ✗ markers, colorized.
- Scopes to a namespace, and supports `--as` user / `--as-group` impersonation.
- Filters with `--verb` and `--resource`; `--json` for automation.

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

`kubectl krew install can` does not work yet. The plugin is not in the krew index,
and the [krew manifest](examples/can.yaml) still carries placeholder sha256 values.
It becomes usable once a release with binaries exists, the manifest is filled in
from that release's `checksums.txt`, and it is submitted to the index. Until then you
can install from the filled-in manifest locally with
`kubectl krew install --manifest=examples/can.yaml`.

## Usage

| Flag | Description |
|------|-------------|
| `--kubeconfig` | Path to a kubeconfig file. Without it the standard loading rules apply: `$KUBECONFIG`, then `~/.kube/config`. The current context of that file is used |
| `-n`, `--namespace` | Namespace to check (empty = cluster scope) |
| `--as` | Impersonate a user |
| `--as-group` | Impersonate a group (repeatable) |
| `--verb` | Only probe these verbs (comma-separated) |
| `--resource` | Only probe these resources (comma-separated) |
| `--json` | Emit JSON |
| `--no-color` | Disable colored output |

## How it works

For every verb×resource pair it issues a `SelfSubjectAccessReview` with the
resource's API group filled in (so `deployments` is probed under `apps`, `ingresses`
under `networking.k8s.io`, and so on), then groups the answers by resource. Because it
asks the API server the same question `kubectl auth can-i` does, the answers reflect
the *real* authorizer — RBAC, webhooks, node restrictions, all of it — not a
client-side guess. The review call sits behind a `Reviewer` interface, so the probe
matrix, filtering, grouping, and rendering are unit-tested with no cluster.

## Development

```bash
make build   # go build -o kubectl-can .
make test    # go test -race ./...
```

## License

MIT — see [LICENSE](LICENSE).

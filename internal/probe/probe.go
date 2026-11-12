// Package probe evaluates what the current context is allowed to do.
package probe

import (
	"context"
	"errors"
	"sort"
)

// Resource is a Kubernetes resource plus its API group ("" for the core group).
// Verbs, when set, lists the verbs the API server serves for the resource (from
// discovery); Probe skips any other verb. Nil means "probe every requested verb".
type Resource struct {
	Name  string
	Group string
	Verbs []string
}

// Qualified returns the kubectl-style name: "deployments.apps", or just "pods"
// for the core group.
func (r Resource) Qualified() string {
	if r.Group == "" {
		return r.Name
	}
	return r.Name + "." + r.Group
}

// ErrPartialDiscovery marks a discovery result that is missing some API groups
// (for example an aggregated API server that is down). The resources that were
// discovered are still usable.
var ErrPartialDiscovery = errors.New("partial API discovery")

// Query is a single access question.
type Query struct {
	Verb      string
	Resource  string
	Group     string
	Namespace string
}

// Result is the answer to a Query.
type Result struct {
	Verb     string `json:"verb"`
	Resource string `json:"resource"`
	Group    string `json:"group,omitempty"`
	Allowed  bool   `json:"allowed"`
	Reason   string `json:"reason,omitempty"`
}

// Reviewer answers a single access Query. It is the seam over
// SelfSubjectAccessReview, so probing is testable without a cluster.
type Reviewer interface {
	Review(ctx context.Context, q Query) (allowed bool, reason string, err error)
}

// DefaultVerbs are the verbs probed unless filtered.
var DefaultVerbs = []string{
	"get", "list", "watch", "create", "update", "patch", "delete", "deletecollection",
}

// DefaultResources are the resources probed unless filtered, with their groups.
var DefaultResources = []Resource{
	{Name: "pods"}, {Name: "services"}, {Name: "configmaps"}, {Name: "secrets"},
	{Name: "persistentvolumeclaims"}, {Name: "serviceaccounts"}, {Name: "events"},
	{Name: "namespaces"}, {Name: "nodes"},
	{Name: "deployments", Group: "apps"}, {Name: "statefulsets", Group: "apps"},
	{Name: "daemonsets", Group: "apps"}, {Name: "replicasets", Group: "apps"},
	{Name: "jobs", Group: "batch"}, {Name: "cronjobs", Group: "batch"},
	{Name: "ingresses", Group: "networking.k8s.io"}, {Name: "networkpolicies", Group: "networking.k8s.io"},
	{Name: "roles", Group: "rbac.authorization.k8s.io"}, {Name: "rolebindings", Group: "rbac.authorization.k8s.io"},
}

// Probe runs the verb×resource matrix through the Reviewer, returning results in
// a stable order (resource, group, then verb by DefaultVerbs order). Verbs a
// resource does not serve (see Resource.Verbs) are not probed.
func Probe(ctx context.Context, r Reviewer, verbs []string, resources []Resource, namespace string) ([]Result, error) {
	verbRank := map[string]int{}
	for i, v := range DefaultVerbs {
		verbRank[v] = i
	}
	var out []Result
	for _, res := range resources {
		served := set(res.Verbs)
		for _, verb := range verbs {
			if res.Verbs != nil && !served[verb] {
				continue
			}
			allowed, reason, err := r.Review(ctx, Query{
				Verb: verb, Resource: res.Name, Group: res.Group, Namespace: namespace,
			})
			if err != nil {
				return nil, err
			}
			out = append(out, Result{
				Verb: verb, Resource: res.Name, Group: res.Group, Allowed: allowed, Reason: reason,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Resource != out[j].Resource {
			return out[i].Resource < out[j].Resource
		}
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return verbRank[out[i].Verb] < verbRank[out[j].Verb]
	})
	return out, nil
}

// Filter keeps only the requested verbs / resources (empty means keep all).
// A requested resource matches by plain name ("pods") or qualified name
// ("certificates.cert-manager.io").
func Filter(verbs []string, resources []Resource, onlyVerbs, onlyResources []string) ([]string, []Resource) {
	if len(onlyVerbs) > 0 {
		want := set(onlyVerbs)
		var kv []string
		for _, v := range verbs {
			if want[v] {
				kv = append(kv, v)
			}
		}
		verbs = kv
	}
	if len(onlyResources) > 0 {
		want := set(onlyResources)
		var kr []Resource
		for _, r := range resources {
			if want[r.Name] || want[r.Qualified()] {
				kr = append(kr, r)
			}
		}
		resources = kr
	}
	return verbs, resources
}

// FilterGroups keeps only resources in the given API groups (empty means keep
// all). "core" selects the core group, which has the empty name.
func FilterGroups(resources []Resource, groups []string) []Resource {
	if len(groups) == 0 {
		return resources
	}
	want := map[string]bool{}
	for _, g := range groups {
		if g == "core" {
			g = ""
		}
		want[g] = true
	}
	var kr []Resource
	for _, r := range resources {
		if want[r.Group] {
			kr = append(kr, r)
		}
	}
	return kr
}

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

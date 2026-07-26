// Package probe evaluates what the current context is allowed to do.
package probe

import (
	"context"
	"sort"
)

// Resource is a Kubernetes resource plus its API group ("" for the core group).
type Resource struct {
	Name  string
	Group string
}

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
	{"pods", ""}, {"services", ""}, {"configmaps", ""}, {"secrets", ""},
	{"persistentvolumeclaims", ""}, {"serviceaccounts", ""}, {"events", ""},
	{"namespaces", ""}, {"nodes", ""},
	{"deployments", "apps"}, {"statefulsets", "apps"}, {"daemonsets", "apps"}, {"replicasets", "apps"},
	{"jobs", "batch"}, {"cronjobs", "batch"},
	{"ingresses", "networking.k8s.io"}, {"networkpolicies", "networking.k8s.io"},
	{"roles", "rbac.authorization.k8s.io"}, {"rolebindings", "rbac.authorization.k8s.io"},
}

// Probe runs the verb×resource matrix through the Reviewer, returning results in
// a stable order (resource, then verb by DefaultVerbs order).
func Probe(ctx context.Context, r Reviewer, verbs []string, resources []Resource, namespace string) ([]Result, error) {
	verbRank := map[string]int{}
	for i, v := range DefaultVerbs {
		verbRank[v] = i
	}
	var out []Result
	for _, res := range resources {
		for _, verb := range verbs {
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
		return verbRank[out[i].Verb] < verbRank[out[j].Verb]
	})
	return out, nil
}

// Filter keeps only the requested verbs / resources (empty means keep all).
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
			if want[r.Name] {
				kr = append(kr, r)
			}
		}
		resources = kr
	}
	return verbs, resources
}

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

package probe

import (
	"context"
	"testing"
)

// fakeReviewer allows a fixed set of "verb resource" combinations.
type fakeReviewer struct {
	allow map[string]bool
	calls int
}

func (f *fakeReviewer) Review(_ context.Context, q Query) (bool, string, error) {
	f.calls++
	key := q.Verb + " " + q.Resource
	if f.allow[key] {
		return true, "RBAC: allowed", nil
	}
	return false, "", nil
}

func TestProbeRunsFullMatrix(t *testing.T) {
	r := &fakeReviewer{allow: map[string]bool{"get pods": true, "list pods": true}}
	verbs := []string{"get", "list", "delete"}
	res := []Resource{{Name: "pods"}, {Name: "secrets"}}
	out, err := Probe(context.Background(), r, verbs, res, "default")
	if err != nil {
		t.Fatal(err)
	}
	if r.calls != 6 {
		t.Errorf("expected 6 reviews, got %d", r.calls)
	}
	allowed := map[string]bool{}
	for _, x := range out {
		allowed[x.Verb+" "+x.Resource] = x.Allowed
	}
	if !allowed["get pods"] || allowed["delete pods"] || allowed["get secrets"] {
		t.Errorf("unexpected allow map: %+v", allowed)
	}
}

func TestProbeStableOrder(t *testing.T) {
	r := &fakeReviewer{allow: map[string]bool{}}
	out, _ := Probe(context.Background(), r, []string{"delete", "get"}, []Resource{{Name: "pods"}}, "")
	// verbs ordered by DefaultVerbs (get before delete).
	if out[0].Verb != "get" || out[1].Verb != "delete" {
		t.Errorf("verbs not in canonical order: %v", []string{out[0].Verb, out[1].Verb})
	}
}

func TestFilterVerbsAndResources(t *testing.T) {
	v, r := Filter(DefaultVerbs, DefaultResources, []string{"get", "delete"}, []string{"pods", "secrets"})
	if len(v) != 2 {
		t.Errorf("verbs = %v", v)
	}
	names := map[string]bool{}
	for _, x := range r {
		names[x.Name] = true
	}
	if len(r) != 2 || !names["pods"] || !names["secrets"] {
		t.Errorf("resources = %+v", r)
	}
}

func TestFilterNoFilterKeepsAll(t *testing.T) {
	v, r := Filter(DefaultVerbs, DefaultResources, nil, nil)
	if len(v) != len(DefaultVerbs) || len(r) != len(DefaultResources) {
		t.Errorf("empty filter should keep all: %d verbs, %d resources", len(v), len(r))
	}
}

func TestResourceGroupPreserved(t *testing.T) {
	r := &fakeReviewer{allow: map[string]bool{}}
	out, _ := Probe(context.Background(), r, []string{"get"}, []Resource{{Name: "deployments", Group: "apps"}}, "")
	if out[0].Group != "apps" {
		t.Errorf("group not preserved: %q", out[0].Group)
	}
}

func TestProbeSkipsVerbsTheResourceDoesNotServe(t *testing.T) {
	r := &fakeReviewer{allow: map[string]bool{}}
	res := []Resource{{Name: "tokenreviews", Group: "authentication.k8s.io", Verbs: []string{"create"}}}
	out, err := Probe(context.Background(), r, DefaultVerbs, res, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.calls != 1 || len(out) != 1 || out[0].Verb != "create" {
		t.Errorf("want only create probed, got %d calls: %+v", r.calls, out)
	}
}

func TestProbeOrdersSameNameByGroup(t *testing.T) {
	r := &fakeReviewer{allow: map[string]bool{}}
	res := []Resource{{Name: "events", Group: "events.k8s.io"}, {Name: "events"}}
	out, _ := Probe(context.Background(), r, []string{"get"}, res, "")
	if out[0].Group != "" || out[1].Group != "events.k8s.io" {
		t.Errorf("same-name resources not ordered by group: %+v", out)
	}
}

func TestFilterMatchesQualifiedName(t *testing.T) {
	all := []Resource{
		{Name: "certificates", Group: "cert-manager.io"},
		{Name: "certificates", Group: "other.example.com"},
		{Name: "pods"},
	}
	_, r := Filter(DefaultVerbs, all, nil, []string{"certificates.cert-manager.io", "pods"})
	if len(r) != 2 || r[0].Group != "cert-manager.io" || r[1].Name != "pods" {
		t.Errorf("resources = %+v", r)
	}
}

func TestFilterGroups(t *testing.T) {
	all := []Resource{{Name: "pods"}, {Name: "applications", Group: "argoproj.io"}, {Name: "jobs", Group: "batch"}}
	if r := FilterGroups(all, []string{"argoproj.io"}); len(r) != 1 || r[0].Name != "applications" {
		t.Errorf("argoproj.io: %+v", r)
	}
	if r := FilterGroups(all, []string{"core", "batch"}); len(r) != 2 || r[0].Name != "pods" || r[1].Name != "jobs" {
		t.Errorf("core,batch: %+v", r)
	}
	if r := FilterGroups(all, nil); len(r) != 3 {
		t.Errorf("no filter: %+v", r)
	}
}

func TestQualified(t *testing.T) {
	if got := (Resource{Name: "pods"}).Qualified(); got != "pods" {
		t.Errorf("core: %q", got)
	}
	if got := (Resource{Name: "deployments", Group: "apps"}).Qualified(); got != "deployments.apps" {
		t.Errorf("apps: %q", got)
	}
}

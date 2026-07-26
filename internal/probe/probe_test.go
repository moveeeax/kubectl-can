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
	res := []Resource{{"pods", ""}, {"secrets", ""}}
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
	out, _ := Probe(context.Background(), r, []string{"delete", "get"}, []Resource{{"pods", ""}}, "")
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
	out, _ := Probe(context.Background(), r, []string{"get"}, []Resource{{"deployments", "apps"}}, "")
	if out[0].Group != "apps" {
		t.Errorf("group not preserved: %q", out[0].Group)
	}
}

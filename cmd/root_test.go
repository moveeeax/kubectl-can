package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moveeeax/kubectl-can/internal/probe"
)

type fakeReviewer struct{ allow map[string]bool }

func (f fakeReviewer) Review(_ context.Context, q probe.Query) (bool, string, error) {
	return f.allow[q.Verb+" "+q.Resource], "", nil
}

func withReviewer(t *testing.T, allow map[string]bool) {
	t.Helper()
	orig := reviewerFactory
	reviewerFactory = func(string, string, []string) (probe.Reviewer, error) {
		return fakeReviewer{allow: allow}, nil
	}
	t.Cleanup(func() { reviewerFactory = orig })
}

func TestRunGrouped(t *testing.T) {
	withReviewer(t, map[string]bool{"get pods": true})
	var out bytes.Buffer
	if err := RunWith([]string{"--verb", "get,delete", "--resource", "pods", "--no-color"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "✔ get") || !strings.Contains(s, "✗ delete") {
		t.Errorf("unexpected output:\n%s", s)
	}
}

func TestRunJSON(t *testing.T) {
	withReviewer(t, map[string]bool{"get pods": true})
	var out bytes.Buffer
	if err := RunWith([]string{"--verb", "get", "--resource", "pods", "--json"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	var res []probe.Result
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(res) != 1 || !res[0].Allowed {
		t.Errorf("unexpected: %+v", res)
	}
}

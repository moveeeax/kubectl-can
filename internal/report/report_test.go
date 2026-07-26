package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moveeeax/kubectl-can/internal/probe"
)

func sample() []probe.Result {
	return []probe.Result{
		{Verb: "get", Resource: "pods", Allowed: true},
		{Verb: "delete", Resource: "pods", Allowed: false},
		{Verb: "get", Resource: "secrets", Allowed: false},
	}
}

func TestGroupedByResource(t *testing.T) {
	var b bytes.Buffer
	Grouped(&b, sample(), false)
	out := b.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 resource lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "✔ get") || !strings.Contains(out, "✗ delete") {
		t.Errorf("markers missing:\n%s", out)
	}
	if !strings.Contains(out, "pods") || !strings.Contains(out, "secrets") {
		t.Errorf("resources missing:\n%s", out)
	}
}

func TestGroupedColor(t *testing.T) {
	var b bytes.Buffer
	Grouped(&b, sample(), true)
	if !strings.Contains(b.String(), "\033[32m") || !strings.Contains(b.String(), "\033[31m") {
		t.Errorf("expected color codes:\n%q", b.String())
	}
}

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, sample()); err != nil {
		t.Fatal(err)
	}
	var got []probe.Result
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 3 || !got[0].Allowed {
		t.Errorf("unexpected: %+v", got)
	}
}

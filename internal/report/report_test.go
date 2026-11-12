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
	Grouped(&b, sample(), false, false)
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
	Grouped(&b, sample(), true, false)
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

func TestGroupedQualify(t *testing.T) {
	results := []probe.Result{
		{Verb: "get", Resource: "pods", Allowed: true},
		{Verb: "patch", Resource: "applications", Group: "argoproj.io", Allowed: true},
	}
	var b bytes.Buffer
	Grouped(&b, results, false, true)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "applications.argoproj.io ") || !strings.HasPrefix(lines[1], "pods ") {
		t.Errorf("unexpected qualified output:\n%s", b.String())
	}
}

func TestGroupedSameNameDifferentGroups(t *testing.T) {
	results := []probe.Result{
		{Verb: "get", Resource: "events", Allowed: true},
		{Verb: "get", Resource: "events", Group: "events.k8s.io", Allowed: false},
		{Verb: "get", Resource: "pods", Allowed: true},
	}
	var b bytes.Buffer
	Grouped(&b, results, false, false)
	out := b.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "events ") || !strings.HasPrefix(lines[1], "events.events.k8s.io ") || !strings.HasPrefix(lines[2], "pods ") {
		t.Errorf("unexpected labels:\n%s", out)
	}
}

func TestGroupedWidensForLongNames(t *testing.T) {
	long := "certificatesigningrequests.certificates.k8s.io"
	results := []probe.Result{
		{Verb: "get", Resource: "certificatesigningrequests", Group: "certificates.k8s.io"},
		{Verb: "get", Resource: "pods"},
	}
	var b bytes.Buffer
	Grouped(&b, results, false, true)
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	// Both marker columns start at the same offset.
	if strings.Index(lines[0], "✗") != len(long)+1 || strings.Index(lines[1], "✗") != len(long)+1 {
		t.Errorf("columns not aligned:\n%s", b.String())
	}
}

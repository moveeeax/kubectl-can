package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/moveeeax/kubectl-can/internal/whocan"
)

func subjects() []whocan.Subject {
	return []whocan.Subject{
		{Kind: "Group", Name: "system:masters", Via: []whocan.Via{
			{BindingKind: "ClusterRoleBinding", BindingName: "cluster-admin", RoleKind: "ClusterRole", RoleName: "cluster-admin"},
		}, Escalations: []string{"bind", "escalate"}},
		{Kind: "ServiceAccount", Name: "ci", Namespace: "prod", Via: []whocan.Via{
			{BindingKind: "RoleBinding", BindingName: "ci", BindingNamespace: "prod", RoleKind: "Role", RoleName: "deployer"},
			{BindingKind: "RoleBinding", BindingName: "ci-secrets", BindingNamespace: "prod", RoleKind: "ClusterRole", RoleName: "secret-reader"},
		}},
	}
}

func TestSubjectsTable(t *testing.T) {
	var b bytes.Buffer
	if err := Subjects(&b, subjects(), true); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("want header + 3 rows, got:\n%s", b.String())
	}
	if !strings.Contains(lines[0], "ESCALATION") {
		t.Errorf("header missing ESCALATION: %q", lines[0])
	}
	if !strings.Contains(lines[1], "ClusterRoleBinding/cluster-admin") || !strings.HasSuffix(lines[1], "bind,escalate") {
		t.Errorf("row 1: %q", lines[1])
	}
	if !strings.Contains(lines[2], "prod/ci") || !strings.Contains(lines[2], "RoleBinding/prod/ci ") || !strings.HasSuffix(lines[2], "-") {
		t.Errorf("row 2: %q", lines[2])
	}
}

func TestSubjectsTableWithoutEscalation(t *testing.T) {
	var b bytes.Buffer
	if err := Subjects(&b, subjects(), false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "ESCALATION") {
		t.Errorf("ESCALATION column shown without the flag:\n%s", b.String())
	}
}

func TestSubjectsTableEmpty(t *testing.T) {
	var b bytes.Buffer
	if err := Subjects(&b, nil, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "no subject") {
		t.Errorf("unexpected: %q", b.String())
	}
}

func TestSubjectsJSON(t *testing.T) {
	var b bytes.Buffer
	if err := SubjectsJSON(&b, subjects()); err != nil {
		t.Fatal(err)
	}
	var got []whocan.Subject
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 2 || len(got[1].Via) != 2 || got[0].Escalations[0] != "bind" {
		t.Errorf("unexpected: %+v", got)
	}
	b.Reset()
	_ = SubjectsJSON(&b, nil)
	if strings.TrimSpace(b.String()) != "[]" {
		t.Errorf("nil should encode as []: %q", b.String())
	}
}

func TestSubjectsCSV(t *testing.T) {
	var b bytes.Buffer
	if err := SubjectsCSV(&b, subjects()); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[0][0] != "kind" {
		t.Fatalf("rows = %v", rows)
	}
	if rows[1][8] != "bind;escalate" || rows[3][4] != "ci-secrets" || rows[3][2] != "prod" {
		t.Errorf("rows = %v", rows)
	}
}

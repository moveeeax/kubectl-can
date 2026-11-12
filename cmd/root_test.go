package cmd

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/moveeeax/kubectl-can/internal/probe"
	"github.com/moveeeax/kubectl-can/internal/whocan"
)

type fakeCluster struct {
	allow        map[string]bool
	resources    []probe.Resource
	discoveryErr error
	snap         whocan.Snapshot
	queries      []probe.Query
	namespaced   *bool
	rbacNS       *string
}

func (f *fakeCluster) Review(_ context.Context, q probe.Query) (bool, string, error) {
	f.queries = append(f.queries, q)
	return f.allow[q.Verb+" "+q.Resource], "", nil
}

func (f *fakeCluster) Resources(_ context.Context, namespacedOnly bool) ([]probe.Resource, error) {
	f.namespaced = &namespacedOnly
	return f.resources, f.discoveryErr
}

func (f *fakeCluster) RBAC(_ context.Context, namespace string) (whocan.Snapshot, error) {
	f.rbacNS = &namespace
	return f.snap, nil
}

func withCluster(t *testing.T, f *fakeCluster) *fakeCluster {
	t.Helper()
	orig := clusterFactory
	clusterFactory = func(string, string, []string) (cluster, error) {
		return f, nil
	}
	t.Cleanup(func() { clusterFactory = orig })
	return f
}

func withReviewer(t *testing.T, allow map[string]bool) *fakeCluster {
	t.Helper()
	return withCluster(t, &fakeCluster{allow: allow})
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

func discovered() []probe.Resource {
	return []probe.Resource{
		{Name: "pods", Verbs: []string{"get", "list", "create", "delete"}},
		{Name: "nodes", Verbs: []string{"get", "list"}},
		{Name: "applications", Group: "argoproj.io", Verbs: []string{"get", "patch"}},
		{Name: "certificates", Group: "cert-manager.io", Verbs: []string{"get", "patch"}},
	}
}

func TestRunAllResources(t *testing.T) {
	f := withCluster(t, &fakeCluster{allow: map[string]bool{"patch applications": true}, resources: discovered()})
	var out bytes.Buffer
	if err := RunWith([]string{"--all-resources", "--no-color"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if f.namespaced == nil || *f.namespaced {
		t.Errorf("cluster-scope run should discover all resources, namespacedOnly=%v", f.namespaced)
	}
	// 4 + 2 + 2 + 2 served verbs, nothing else.
	if len(f.queries) != 10 {
		t.Errorf("want 10 reviews, got %d: %+v", len(f.queries), f.queries)
	}
	s := out.String()
	if !strings.Contains(s, "applications.argoproj.io") || !strings.Contains(s, "✔ patch") {
		t.Errorf("CRD missing or not qualified:\n%s", s)
	}
	for _, q := range f.queries {
		if q.Resource == "applications" && q.Group != "argoproj.io" {
			t.Errorf("CRD probed without its group: %+v", q)
		}
	}
}

func TestRunAllResourcesNamespaced(t *testing.T) {
	f := withCluster(t, &fakeCluster{resources: discovered()})
	if err := RunWith([]string{"--all-resources", "-n", "prod"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if f.namespaced == nil || !*f.namespaced {
		t.Errorf("namespaced run should discover only namespaced resources")
	}
}

func TestRunAllResourcesGroupFilter(t *testing.T) {
	f := withCluster(t, &fakeCluster{resources: discovered()})
	if err := RunWith([]string{"--all-resources", "--group", "argoproj.io", "--json"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, q := range f.queries {
		if q.Group != "argoproj.io" {
			t.Errorf("probed outside --group: %+v", q)
		}
	}
	if len(f.queries) != 2 {
		t.Errorf("want 2 reviews, got %d", len(f.queries))
	}
}

func TestRunGroupFilterOnDefaults(t *testing.T) {
	f := withReviewer(t, nil)
	if err := RunWith([]string{"--group", "batch", "--verb", "get"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(f.queries) != 2 {
		t.Errorf("want jobs and cronjobs, got %+v", f.queries)
	}
}

func TestRunAllResourcesPartialDiscovery(t *testing.T) {
	withCluster(t, &fakeCluster{
		resources:    discovered(),
		discoveryErr: fmt.Errorf("%w: metrics.k8s.io/v1beta1: unavailable", probe.ErrPartialDiscovery),
	})
	var out bytes.Buffer
	if err := RunWith([]string{"--all-resources", "--no-color"}, &out); err != nil {
		t.Fatalf("partial discovery should not fail the run: %v", err)
	}
	if !strings.Contains(out.String(), "warning") || !strings.Contains(out.String(), "pods") {
		t.Errorf("unexpected output:\n%s", out.String())
	}
}

func TestRunAllResourcesDiscoveryError(t *testing.T) {
	withCluster(t, &fakeCluster{discoveryErr: fmt.Errorf("connection refused")})
	if err := RunWith([]string{"--all-resources"}, &bytes.Buffer{}); err == nil {
		t.Fatal("want error")
	}
}

func TestRunDefaultDoesNotDiscover(t *testing.T) {
	f := withReviewer(t, nil)
	if err := RunWith([]string{"--verb", "get"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if f.namespaced != nil {
		t.Errorf("default run called discovery")
	}
	if len(f.queries) != len(probe.DefaultResources) {
		t.Errorf("want %d reviews, got %d", len(probe.DefaultResources), len(f.queries))
	}
}

func whoSnapshot() whocan.Snapshot {
	return whocan.Snapshot{
		ClusterRoles: []rbacv1.ClusterRole{
			{ObjectMeta: metav1.ObjectMeta{Name: "admin"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"*"}, Resources: []string{"*"}, Verbs: []string{"*"}}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "argo"}, Rules: []rbacv1.PolicyRule{{APIGroups: []string{"argoproj.io"}, Resources: []string{"applications"}, Verbs: []string{"patch"}}}},
		},
		ClusterRoleBindings: []rbacv1.ClusterRoleBinding{
			{ObjectMeta: metav1.ObjectMeta{Name: "admins"}, RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "admin"},
				Subjects: []rbacv1.Subject{{Kind: "Group", Name: "system:masters"}}},
		},
		RoleBindings: []rbacv1.RoleBinding{
			{ObjectMeta: metav1.ObjectMeta{Namespace: "argocd", Name: "deployer"}, RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "argo"},
				Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "ci", Name: "deployer"}}},
		},
	}
}

func TestWhoText(t *testing.T) {
	f := withCluster(t, &fakeCluster{snap: whoSnapshot()})
	var out bytes.Buffer
	if err := RunWith([]string{"who", "get", "secrets", "-n", "argocd", "--show-escalation"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if f.rbacNS == nil || *f.rbacNS != "argocd" {
		t.Errorf("RBAC read for namespace %v", f.rbacNS)
	}
	s := out.String()
	if !strings.Contains(s, "system:masters") || strings.Contains(s, "ci/deployer") {
		t.Errorf("unexpected subjects:\n%s", s)
	}
	if !strings.Contains(s, "ESCALATION") || !strings.Contains(s, "bind,escalate,impersonate,read-secrets") {
		t.Errorf("escalations missing:\n%s", s)
	}
}

func TestWhoResolvesGroupByDiscovery(t *testing.T) {
	withCluster(t, &fakeCluster{snap: whoSnapshot(), resources: discovered()})
	var out bytes.Buffer
	if err := RunWith([]string{"who", "patch", "applications", "-n", "argocd", "-o", "json"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	var got []whocan.Subject
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(got) != 2 || got[1].Kind != "ServiceAccount" || got[1].Namespace != "ci" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestWhoAmbiguousResource(t *testing.T) {
	res := append(discovered(), probe.Resource{Name: "applications", Group: "app.k8s.io", Verbs: []string{"get"}})
	withCluster(t, &fakeCluster{snap: whoSnapshot(), resources: res})
	err := RunWith([]string{"who", "patch", "applications"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "applications.argoproj.io") {
		t.Errorf("want ambiguity error naming the choices, got %v", err)
	}
}

func TestWhoUnknownResource(t *testing.T) {
	withCluster(t, &fakeCluster{snap: whoSnapshot(), resources: discovered()})
	if err := RunWith([]string{"who", "get", "widgets"}, &bytes.Buffer{}); err == nil {
		t.Error("want error for a resource the cluster does not serve")
	}
}

func TestWhoCSV(t *testing.T) {
	withCluster(t, &fakeCluster{snap: whoSnapshot()})
	var out bytes.Buffer
	if err := RunWith([]string{"who", "patch", "applications.argoproj.io", "-n", "argocd", "-o", "csv"}, &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[2][1] != "deployer" || rows[2][4] != "deployer" {
		t.Errorf("rows = %v", rows)
	}
}

func TestWhoBadOutput(t *testing.T) {
	withCluster(t, &fakeCluster{})
	if err := RunWith([]string{"who", "get", "pods", "-o", "yaml"}, &bytes.Buffer{}); err == nil {
		t.Error("want error for unknown --output")
	}
}

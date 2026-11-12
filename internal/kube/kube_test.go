package kube

import (
	"context"
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/moveeeax/kubectl-can/internal/probe"
)

func TestFromDiscovery(t *testing.T) {
	lists := []*metav1.APIResourceList{
		{GroupVersion: "v1", APIResources: []metav1.APIResource{
			{Name: "pods", Namespaced: true, Verbs: []string{"get", "list", "create"}},
			{Name: "pods/exec", Namespaced: true, Verbs: []string{"create"}},
			{Name: "nodes", Namespaced: false, Verbs: []string{"get", "list"}},
			{Name: "bindings", Namespaced: true, Verbs: nil},
		}},
		{GroupVersion: "argoproj.io/v1alpha1", APIResources: []metav1.APIResource{
			{Name: "applications", Namespaced: true, Verbs: []string{"get", "patch"}},
		}},
		{GroupVersion: "events.k8s.io/v1", APIResources: []metav1.APIResource{
			{Name: "events", Namespaced: true, Verbs: []string{"get"}},
		}},
		nil,
		{GroupVersion: "not/a/valid/gv"},
	}

	got := FromDiscovery(lists, false)
	want := []probe.Resource{
		{Name: "nodes", Verbs: []string{"get", "list"}},
		{Name: "pods", Verbs: []string{"get", "list", "create"}},
		{Name: "applications", Group: "argoproj.io", Verbs: []string{"get", "patch"}},
		{Name: "events", Group: "events.k8s.io", Verbs: []string{"get"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("all:\n got %+v\nwant %+v", got, want)
	}

	for _, r := range FromDiscovery(lists, true) {
		if r.Name == "nodes" {
			t.Errorf("cluster-scoped resource kept with namespacedOnly")
		}
	}
}

func TestRBACNamespaced(t *testing.T) {
	cs := fake.NewClientset(
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "view"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "view"}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "deployer"}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: "dev", Name: "deployer"}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "ci"}},
	)
	snap, err := NewForClientset(cs).RBAC(context.Background(), "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.ClusterRoles) != 1 || len(snap.ClusterRoleBindings) != 1 || len(snap.Roles) != 1 || len(snap.RoleBindings) != 1 {
		t.Errorf("unexpected snapshot: %+v", snap)
	}
	if snap.Roles[0].Namespace != "prod" {
		t.Errorf("role from another namespace: %+v", snap.Roles[0])
	}
}

func TestRBACClusterScopeSkipsRoles(t *testing.T) {
	cs := fake.NewClientset(&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Namespace: "prod", Name: "deployer"}})
	snap, err := NewForClientset(cs).RBAC(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Roles) != 0 || len(snap.RoleBindings) != 0 {
		t.Errorf("cluster-scope snapshot read namespaced objects: %+v", snap)
	}
}

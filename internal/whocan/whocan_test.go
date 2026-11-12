package whocan

import (
	"reflect"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func rule(groups, resources, verbs []string, names ...string) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{APIGroups: groups, Resources: resources, Verbs: verbs, ResourceNames: names}
}

func list(xs ...string) []string { return xs }

func TestRuleAllows(t *testing.T) {
	cases := []struct {
		name string
		rule rbacv1.PolicyRule
		req  Request
		want bool
	}{
		{"exact", rule(list(""), list("secrets"), list("get")), Request{Verb: "get", Resource: "secrets"}, true},
		{"wrong verb", rule(list(""), list("secrets"), list("list")), Request{Verb: "get", Resource: "secrets"}, false},
		{"wrong group", rule(list("apps"), list("secrets"), list("get")), Request{Verb: "get", Resource: "secrets"}, false},
		{"verb wildcard", rule(list(""), list("secrets"), list("*")), Request{Verb: "delete", Resource: "secrets"}, true},
		{"group wildcard", rule(list("*"), list("deployments"), list("get")), Request{Verb: "get", Group: "apps", Resource: "deployments"}, true},
		{"resource wildcard covers subresource", rule(list(""), list("*"), list("create")), Request{Verb: "create", Resource: "pods", Subresource: "exec"}, true},
		{"parent does not cover subresource", rule(list(""), list("pods"), list("create")), Request{Verb: "create", Resource: "pods", Subresource: "exec"}, false},
		{"exact subresource", rule(list(""), list("pods/exec"), list("create")), Request{Verb: "create", Resource: "pods", Subresource: "exec"}, true},
		{"star subresource", rule(list(""), list("*/exec"), list("create")), Request{Verb: "create", Resource: "pods", Subresource: "exec"}, true},
		{"star subresource needs subresource", rule(list(""), list("*/exec"), list("create")), Request{Verb: "create", Resource: "pods"}, false},
		{"resourceNames hit", rule(list(""), list("secrets"), list("get"), "db"), Request{Verb: "get", Resource: "secrets", Name: "db"}, true},
		{"resourceNames miss", rule(list(""), list("secrets"), list("get"), "db"), Request{Verb: "get", Resource: "secrets", Name: "other"}, false},
		{"resourceNames vs any object", rule(list(""), list("secrets"), list("get"), "db"), Request{Verb: "get", Resource: "secrets"}, false},
		{"no resourceNames matches named request", rule(list(""), list("secrets"), list("get")), Request{Verb: "get", Resource: "secrets", Name: "db"}, true},
		{"non-resource rule", rbacv1.PolicyRule{NonResourceURLs: list("/healthz"), Verbs: list("get")}, Request{Verb: "get", Resource: "secrets"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RuleAllows(&c.rule, c.req); got != c.want {
				t.Errorf("RuleAllows = %v, want %v", got, c.want)
			}
		})
	}
}

func sa(ns, name string) rbacv1.Subject {
	return rbacv1.Subject{Kind: rbacv1.ServiceAccountKind, Name: name, Namespace: ns}
}

func user(name string) rbacv1.Subject {
	return rbacv1.Subject{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: name}
}

func group(name string) rbacv1.Subject {
	return rbacv1.Subject{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: name}
}

func meta(ns, name string) metav1.ObjectMeta { return metav1.ObjectMeta{Namespace: ns, Name: name} }

func snapshot() Snapshot {
	return Snapshot{
		ClusterRoles: []rbacv1.ClusterRole{
			{ObjectMeta: meta("", "cluster-admin"), Rules: []rbacv1.PolicyRule{rule(list("*"), list("*"), list("*"))}},
			{ObjectMeta: meta("", "secret-reader"), Rules: []rbacv1.PolicyRule{rule(list(""), list("secrets"), list("get", "list"))}},
			{ObjectMeta: meta("", "pod-reader"), Rules: []rbacv1.PolicyRule{rule(list(""), list("pods"), list("get"))}},
		},
		ClusterRoleBindings: []rbacv1.ClusterRoleBinding{
			{ObjectMeta: meta("", "admins"), RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"}, Subjects: []rbacv1.Subject{group("system:masters")}},
			{ObjectMeta: meta("", "readers"), RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "pod-reader"}, Subjects: []rbacv1.Subject{user("bob")}},
			{ObjectMeta: meta("", "dangling"), RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "missing"}, Subjects: []rbacv1.Subject{user("eve")}},
		},
		Roles: []rbacv1.Role{
			{ObjectMeta: meta("prod", "deployer"), Rules: []rbacv1.PolicyRule{
				rule(list("apps"), list("deployments"), list("get", "patch")),
				rule(list(rbacv1.GroupName), list("roles"), list("bind")),
			}},
			{ObjectMeta: meta("dev", "deployer"), Rules: []rbacv1.PolicyRule{rule(list(""), list("secrets"), list("*"))}},
		},
		RoleBindings: []rbacv1.RoleBinding{
			{ObjectMeta: meta("prod", "ci"), RoleRef: rbacv1.RoleRef{Kind: "Role", Name: "deployer"}, Subjects: []rbacv1.Subject{sa("prod", "ci")}},
			{ObjectMeta: meta("prod", "ci-secrets"), RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "secret-reader"}, Subjects: []rbacv1.Subject{sa("prod", "ci"), user("alice")}},
			{ObjectMeta: meta("dev", "dev"), RoleRef: rbacv1.RoleRef{Kind: "Role", Name: "deployer"}, Subjects: []rbacv1.Subject{user("mallory")}},
		},
	}
}

func names(subjects []Subject) []string {
	var out []string
	for _, s := range subjects {
		n := s.Kind + ":" + s.Name
		if s.Namespace != "" {
			n = s.Kind + ":" + s.Namespace + "/" + s.Name
		}
		out = append(out, n)
	}
	return out
}

func TestLookupNamespaced(t *testing.T) {
	got := Lookup(snapshot(), Request{Verb: "get", Resource: "secrets", Namespace: "prod"}, false)
	want := []string{"Group:system:masters", "ServiceAccount:prod/ci", "User:alice"}
	if !reflect.DeepEqual(names(got), want) {
		t.Errorf("subjects = %v, want %v", names(got), want)
	}
	// RoleBindings of other namespaces (mallory in dev) must not leak in.
	for _, s := range got {
		if s.Name == "mallory" {
			t.Errorf("dev binding leaked into prod lookup")
		}
	}
}

func TestLookupClusterScopeIgnoresRoleBindings(t *testing.T) {
	got := Lookup(snapshot(), Request{Verb: "get", Resource: "secrets"}, false)
	if want := []string{"Group:system:masters"}; !reflect.DeepEqual(names(got), want) {
		t.Errorf("subjects = %v, want %v", names(got), want)
	}
}

func TestLookupVia(t *testing.T) {
	got := Lookup(snapshot(), Request{Verb: "patch", Group: "apps", Resource: "deployments", Namespace: "prod"}, false)
	var ci *Subject
	for i := range got {
		if got[i].Name == "ci" {
			ci = &got[i]
		}
	}
	if ci == nil {
		t.Fatalf("ci not found in %v", names(got))
	}
	want := []Via{{BindingKind: "RoleBinding", BindingName: "ci", BindingNamespace: "prod", RoleKind: "Role", RoleName: "deployer"}}
	if !reflect.DeepEqual(ci.Via, want) {
		t.Errorf("via = %+v, want %+v", ci.Via, want)
	}
}

func TestLookupSkipsDanglingRoleRef(t *testing.T) {
	got := Lookup(snapshot(), Request{Verb: "get", Resource: "pods"}, false)
	for _, s := range got {
		if s.Name == "eve" {
			t.Errorf("binding to a missing ClusterRole granted access")
		}
	}
}

func TestLookupEscalations(t *testing.T) {
	got := Lookup(snapshot(), Request{Verb: "patch", Group: "apps", Resource: "deployments", Namespace: "prod"}, true)
	esc := map[string][]string{}
	for _, s := range got {
		esc[s.Name] = s.Escalations
	}
	if want := []string{"bind", "read-secrets"}; !reflect.DeepEqual(esc["ci"], want) {
		t.Errorf("ci escalations = %v, want %v", esc["ci"], want)
	}
	if want := []string{"bind", "escalate", "impersonate", "read-secrets"}; !reflect.DeepEqual(esc["system:masters"], want) {
		t.Errorf("system:masters escalations = %v, want %v", esc["system:masters"], want)
	}
	plain := Lookup(snapshot(), Request{Verb: "patch", Group: "apps", Resource: "deployments", Namespace: "prod"}, false)
	for _, s := range plain {
		if s.Escalations != nil {
			t.Errorf("escalations reported without the flag: %+v", s)
		}
	}
}

func TestLookupAggregatedClusterRole(t *testing.T) {
	snap := Snapshot{
		ClusterRoles: []rbacv1.ClusterRole{
			{
				ObjectMeta:      meta("", "monitoring"),
				AggregationRule: &rbacv1.AggregationRule{ClusterRoleSelectors: []metav1.LabelSelector{{MatchLabels: map[string]string{"aggregate-to-monitoring": "true"}}}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "monitoring-endpoints", Labels: map[string]string{"aggregate-to-monitoring": "true"}},
				Rules:      []rbacv1.PolicyRule{rule(list(""), list("endpoints"), list("list"))},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Labels: map[string]string{"other": "true"}},
				Rules:      []rbacv1.PolicyRule{rule(list(""), list("secrets"), list("list"))},
			},
		},
		ClusterRoleBindings: []rbacv1.ClusterRoleBinding{
			{ObjectMeta: meta("", "prom"), RoleRef: rbacv1.RoleRef{Kind: "ClusterRole", Name: "monitoring"}, Subjects: []rbacv1.Subject{sa("monitoring", "prometheus")}},
		},
	}
	if got := Lookup(snap, Request{Verb: "list", Resource: "endpoints"}, false); len(got) != 1 || got[0].Name != "prometheus" {
		t.Errorf("aggregated rule not expanded: %v", names(got))
	}
	if got := Lookup(snap, Request{Verb: "list", Resource: "secrets"}, false); len(got) != 0 {
		t.Errorf("non-matching role aggregated: %v", names(got))
	}
}

func TestLookupMergesBindingsPerSubject(t *testing.T) {
	snap := snapshot()
	snap.RoleBindings = append(snap.RoleBindings, rbacv1.RoleBinding{
		ObjectMeta: meta("prod", "ci-admin"),
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Name: "ci"}}, // namespace defaults to the binding's
	})
	got := Lookup(snap, Request{Verb: "get", Resource: "secrets", Namespace: "prod"}, false)
	for _, s := range got {
		if s.Name == "ci" {
			if s.Namespace != "prod" || len(s.Via) != 2 {
				t.Errorf("ci = %+v", s)
			}
			return
		}
	}
	t.Errorf("ci not found in %v", names(got))
}

func TestParseResource(t *testing.T) {
	cases := []struct {
		in              string
		res, group, sub string
		hasGroup        bool
	}{
		{"pods", "pods", "", "", false},
		{"pods/exec", "pods", "", "exec", false},
		{"deployments.apps", "deployments", "apps", "", true},
		{"certificates.cert-manager.io/status", "certificates", "cert-manager.io", "status", true},
	}
	for _, c := range cases {
		res, group, sub, hasGroup := ParseResource(c.in)
		if res != c.res || group != c.group || sub != c.sub || hasGroup != c.hasGroup {
			t.Errorf("ParseResource(%q) = %q %q %q %v", c.in, res, group, sub, hasGroup)
		}
	}
}

// Package whocan answers the reverse RBAC question: given a verb and a resource,
// which subjects (users, groups, ServiceAccounts) are granted it by RBAC.
//
// It evaluates RBAC objects read from the cluster, matching rules the same way
// the RBAC authorizer does (wildcards, resourceNames, subresources and the
// "*/subresource" form). Unlike SelfSubjectAccessReview it only sees RBAC:
// grants from webhook or Node authorizers are not visible here.
package whocan

import (
	"sort"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// Snapshot is the set of RBAC objects a lookup is evaluated against. For a
// namespaced lookup Roles and RoleBindings hold that namespace's objects; for a
// cluster-scope lookup they are ignored.
type Snapshot struct {
	ClusterRoles        []rbacv1.ClusterRole
	ClusterRoleBindings []rbacv1.ClusterRoleBinding
	Roles               []rbacv1.Role
	RoleBindings        []rbacv1.RoleBinding
}

// Request is the access being looked up.
type Request struct {
	Verb        string
	Group       string
	Resource    string
	Subresource string
	Name        string // resource name; empty = any object
	Namespace   string // empty = cluster scope
}

// Via is one binding that grants the request to a subject.
type Via struct {
	BindingKind      string `json:"binding_kind"`
	BindingName      string `json:"binding_name"`
	BindingNamespace string `json:"binding_namespace,omitempty"`
	RoleKind         string `json:"role_kind"`
	RoleName         string `json:"role_name"`
}

// Subject is one subject granted the request, with every binding that grants it.
type Subject struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Namespace   string   `json:"namespace,omitempty"`
	Via         []Via    `json:"via"`
	Escalations []string `json:"escalations,omitempty"`
}

// Escalation is a permission that lets its holder gain more access than it was
// granted directly.
type Escalation struct {
	Name     string
	Group    string
	Verbs    []string
	Resource []string
}

// Escalations are the checks reported with Lookup's withEscalation flag:
// bind and escalate on roles let a subject grant itself any permission,
// impersonate lets it act as another identity, and reading secrets exposes
// ServiceAccount tokens and other credentials.
var Escalations = []Escalation{
	{Name: "bind", Group: rbacv1.GroupName, Verbs: []string{"bind"}, Resource: []string{"roles", "clusterroles"}},
	{Name: "escalate", Group: rbacv1.GroupName, Verbs: []string{"escalate"}, Resource: []string{"roles", "clusterroles"}},
	{Name: "impersonate", Group: "", Verbs: []string{"impersonate"}, Resource: []string{"users", "groups", "serviceaccounts"}},
	{Name: "read-secrets", Group: "", Verbs: []string{"get", "list", "watch"}, Resource: []string{"secrets"}},
}

type grant struct {
	via   Via
	rules []rbacv1.PolicyRule
}

type subjectKey struct{ kind, name, namespace string }

// Lookup returns every subject the snapshot grants req to, sorted by kind,
// namespace and name. With withEscalation each subject also lists the
// Escalations it holds in the same scope.
func Lookup(snap Snapshot, req Request, withEscalation bool) []Subject {
	grants := grantsInScope(snap, req.Namespace)

	bySubject := map[subjectKey][]grant{}
	var keys []subjectKey
	for _, g := range grants {
		for _, s := range g.subjects {
			k := keyOf(s, g.via.BindingNamespace)
			if _, ok := bySubject[k]; !ok {
				keys = append(keys, k)
			}
			bySubject[k] = append(bySubject[k], g.grant)
		}
	}

	var out []Subject
	for _, k := range keys {
		var via []Via
		for _, g := range bySubject[k] {
			if RulesAllow(g.rules, req) {
				via = append(via, g.via)
			}
		}
		if len(via) == 0 {
			continue
		}
		s := Subject{Kind: k.kind, Name: k.name, Namespace: k.namespace, Via: via}
		if withEscalation {
			s.Escalations = escalations(bySubject[k], req.Namespace)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

type boundGrant struct {
	grant
	subjects []rbacv1.Subject
}

// grantsInScope resolves the bindings that apply in namespace: every
// ClusterRoleBinding, plus that namespace's RoleBindings when namespace is set.
func grantsInScope(snap Snapshot, namespace string) []boundGrant {
	clusterRoles := map[string][]rbacv1.PolicyRule{}
	for _, cr := range snap.ClusterRoles {
		clusterRoles[cr.Name] = clusterRoleRules(cr, snap.ClusterRoles)
	}
	roles := map[string][]rbacv1.PolicyRule{}
	for _, r := range snap.Roles {
		if r.Namespace == namespace {
			roles[r.Name] = r.Rules
		}
	}

	var out []boundGrant
	for _, b := range snap.ClusterRoleBindings {
		if b.RoleRef.Kind != "ClusterRole" {
			continue
		}
		rules, ok := clusterRoles[b.RoleRef.Name]
		if !ok {
			continue
		}
		out = append(out, boundGrant{
			grant:    grant{via: Via{BindingKind: "ClusterRoleBinding", BindingName: b.Name, RoleKind: "ClusterRole", RoleName: b.RoleRef.Name}, rules: rules},
			subjects: b.Subjects,
		})
	}
	if namespace == "" {
		return out
	}
	for _, b := range snap.RoleBindings {
		if b.Namespace != namespace {
			continue
		}
		var rules []rbacv1.PolicyRule
		var ok bool
		switch b.RoleRef.Kind {
		case "ClusterRole":
			rules, ok = clusterRoles[b.RoleRef.Name]
		case "Role":
			rules, ok = roles[b.RoleRef.Name]
		}
		if !ok {
			continue
		}
		out = append(out, boundGrant{
			grant:    grant{via: Via{BindingKind: "RoleBinding", BindingName: b.Name, BindingNamespace: b.Namespace, RoleKind: b.RoleRef.Kind, RoleName: b.RoleRef.Name}, rules: rules},
			subjects: b.Subjects,
		})
	}
	return out
}

// clusterRoleRules returns cr's rules, expanding its aggregationRule: the
// rules of every ClusterRole whose labels match one of the selectors are added.
// The aggregation controller writes the same union into cr.Rules, so this only
// matters when the controller has not caught up yet; duplicates are harmless.
func clusterRoleRules(cr rbacv1.ClusterRole, all []rbacv1.ClusterRole) []rbacv1.PolicyRule {
	rules := append([]rbacv1.PolicyRule(nil), cr.Rules...)
	if cr.AggregationRule == nil {
		return rules
	}
	var selectors []labels.Selector
	for _, ls := range cr.AggregationRule.ClusterRoleSelectors {
		sel, err := metav1.LabelSelectorAsSelector(&ls)
		if err != nil {
			continue
		}
		selectors = append(selectors, sel)
	}
	for _, other := range all {
		if other.Name == cr.Name {
			continue
		}
		for _, sel := range selectors {
			if sel.Matches(labels.Set(other.Labels)) {
				rules = append(rules, other.Rules...)
				break
			}
		}
	}
	return rules
}

func keyOf(s rbacv1.Subject, bindingNamespace string) subjectKey {
	k := subjectKey{kind: s.Kind, name: s.Name}
	if s.Kind == rbacv1.ServiceAccountKind {
		k.namespace = s.Namespace
		if k.namespace == "" {
			k.namespace = bindingNamespace
		}
	}
	return k
}

func escalations(grants []grant, namespace string) []string {
	var out []string
	for _, e := range Escalations {
		if holds(grants, e, namespace) {
			out = append(out, e.Name)
		}
	}
	return out
}

func holds(grants []grant, e Escalation, namespace string) bool {
	for _, g := range grants {
		for _, verb := range e.Verbs {
			for _, res := range e.Resource {
				if RulesAllow(g.rules, Request{Verb: verb, Group: e.Group, Resource: res, Namespace: namespace}) {
					return true
				}
			}
		}
	}
	return false
}

// RulesAllow reports whether any rule grants req.
func RulesAllow(rules []rbacv1.PolicyRule, req Request) bool {
	for i := range rules {
		if RuleAllows(&rules[i], req) {
			return true
		}
	}
	return false
}

// RuleAllows reports whether rule grants req, following the RBAC authorizer:
// verb, API group, resource (with subresource) and resource name must all match.
func RuleAllows(rule *rbacv1.PolicyRule, req Request) bool {
	combined := req.Resource
	if req.Subresource != "" {
		combined += "/" + req.Subresource
	}
	return verbMatches(rule, req.Verb) &&
		groupMatches(rule, req.Group) &&
		resourceMatches(rule, combined, req.Subresource) &&
		nameMatches(rule, req.Name)
}

func verbMatches(rule *rbacv1.PolicyRule, verb string) bool {
	for _, v := range rule.Verbs {
		if v == rbacv1.VerbAll || v == verb {
			return true
		}
	}
	return false
}

func groupMatches(rule *rbacv1.PolicyRule, group string) bool {
	for _, g := range rule.APIGroups {
		if g == rbacv1.APIGroupAll || g == group {
			return true
		}
	}
	return false
}

// resourceMatches accepts "*", an exact "resource" or "resource/subresource",
// and "*/subresource" when a subresource is requested.
func resourceMatches(rule *rbacv1.PolicyRule, combined, subresource string) bool {
	for _, r := range rule.Resources {
		if r == rbacv1.ResourceAll || r == combined {
			return true
		}
		if subresource != "" && r == "*/"+subresource {
			return true
		}
	}
	return false
}

// nameMatches: a rule without resourceNames matches any name; a rule with
// resourceNames only matches a request for one of those names, never a request
// for "any object".
func nameMatches(rule *rbacv1.PolicyRule, name string) bool {
	if len(rule.ResourceNames) == 0 {
		return true
	}
	for _, n := range rule.ResourceNames {
		if n == name {
			return true
		}
	}
	return false
}

// ParseResource splits "resource[.group][/subresource]" as accepted on the
// command line, e.g. "pods/exec", "deployments.apps",
// "certificates.cert-manager.io/status". hasGroup reports whether a group was
// given (the core group has the empty name, so "" alone is ambiguous).
func ParseResource(s string) (resource, group, subresource string, hasGroup bool) {
	resource = s
	if i := strings.Index(resource, "/"); i >= 0 {
		resource, subresource = resource[:i], resource[i+1:]
	}
	if i := strings.Index(resource, "."); i >= 0 {
		resource, group, hasGroup = resource[:i], resource[i+1:], true
	}
	return resource, group, subresource, hasGroup
}

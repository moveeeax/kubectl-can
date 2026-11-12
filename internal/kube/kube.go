// Package kube provides a SelfSubjectAccessReview-backed Reviewer, API
// discovery for --all-resources, and RBAC object listing for `who`.
package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/moveeeax/kubectl-can/internal/probe"
	"github.com/moveeeax/kubectl-can/internal/whocan"
)

// Client answers access queries via SelfSubjectAccessReview.
type Client struct {
	cs kubernetes.Interface
}

// New builds a Client from the default kubeconfig loading rules, applying
// optional `--as` user / `--as-group` impersonation.
func New(kubeconfig, asUser string, asGroups []string) (*Client, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	if asUser != "" || len(asGroups) > 0 {
		cfg.Impersonate = rest.ImpersonationConfig{UserName: asUser, Groups: asGroups}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &Client{cs: cs}, nil
}

// NewForClientset wraps an existing clientset (used by tests with the fake one).
func NewForClientset(cs kubernetes.Interface) *Client {
	return &Client{cs: cs}
}

// Review issues a SelfSubjectAccessReview for the query.
func (c *Client) Review(ctx context.Context, q probe.Query) (bool, string, error) {
	ssar := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Namespace: q.Namespace,
				Verb:      q.Verb,
				Group:     q.Group,
				Resource:  q.Resource,
			},
		},
	}
	res, err := c.cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, ssar, metav1.CreateOptions{})
	if err != nil {
		return false, "", err
	}
	return res.Status.Allowed, res.Status.Reason, nil
}

// Resources lists every resource the API server serves, CRDs included, at each
// group's preferred version (discovery's ServerPreferredResources). With
// namespacedOnly, cluster-scoped resources are dropped. If some groups fail
// discovery the resources found so far are returned with an error wrapping
// probe.ErrPartialDiscovery.
func (c *Client) Resources(ctx context.Context, namespacedOnly bool) ([]probe.Resource, error) {
	lists, err := discovery.ServerPreferredResourcesWithContext(ctx, discovery.ToDiscoveryInterfaceWithContext(c.cs.Discovery()))
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) && len(lists) > 0 {
			return FromDiscovery(lists, namespacedOnly), fmt.Errorf("%w: %v", probe.ErrPartialDiscovery, err)
		}
		return nil, fmt.Errorf("discover API resources: %w", err)
	}
	return FromDiscovery(lists, namespacedOnly), nil
}

// FromDiscovery converts discovery lists to probe resources: subresources
// ("pods/exec") and resources without verbs are skipped, each resource keeps the
// verbs it serves, and the result is sorted by group, then name.
func FromDiscovery(lists []*metav1.APIResourceList, namespacedOnly bool) []probe.Resource {
	seen := map[string]bool{}
	var out []probe.Resource
	for _, l := range lists {
		if l == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if strings.Contains(r.Name, "/") || len(r.Verbs) == 0 {
				continue
			}
			if namespacedOnly && !r.Namespaced {
				continue
			}
			res := probe.Resource{Name: r.Name, Group: gv.Group, Verbs: append([]string(nil), r.Verbs...)}
			if seen[res.Qualified()] {
				continue
			}
			seen[res.Qualified()] = true
			out = append(out, res)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// RBAC reads the RBAC objects a who-can lookup needs: all ClusterRoles and
// ClusterRoleBindings, plus the Roles and RoleBindings of namespace when it is
// set. The caller needs list access to those objects.
func (c *Client) RBAC(ctx context.Context, namespace string) (whocan.Snapshot, error) {
	var snap whocan.Snapshot
	rbac := c.cs.RbacV1()
	crs, err := rbac.ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return snap, fmt.Errorf("list clusterroles: %w", err)
	}
	snap.ClusterRoles = crs.Items
	crbs, err := rbac.ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return snap, fmt.Errorf("list clusterrolebindings: %w", err)
	}
	snap.ClusterRoleBindings = crbs.Items
	if namespace == "" {
		return snap, nil
	}
	roles, err := rbac.Roles(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return snap, fmt.Errorf("list roles in %s: %w", namespace, err)
	}
	snap.Roles = roles.Items
	rbs, err := rbac.RoleBindings(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return snap, fmt.Errorf("list rolebindings in %s: %w", namespace, err)
	}
	snap.RoleBindings = rbs.Items
	return snap, nil
}

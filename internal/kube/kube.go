// Package kube provides a SelfSubjectAccessReview-backed Reviewer.
package kube

import (
	"context"
	"fmt"

	authv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/moveeeax/kubectl-can/internal/probe"
)

// Client answers access queries via SelfSubjectAccessReview.
type Client struct {
	cs *kubernetes.Clientset
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

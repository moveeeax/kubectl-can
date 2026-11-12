// Package cmd wires the kubectl-can CLI (installable as `kubectl can` via krew).
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/moveeeax/kubectl-can/internal/kube"
	"github.com/moveeeax/kubectl-can/internal/probe"
	"github.com/moveeeax/kubectl-can/internal/report"
	"github.com/moveeeax/kubectl-can/internal/whocan"
)

// cluster is everything the commands need from the API server.
type cluster interface {
	probe.Reviewer
	Resources(ctx context.Context, namespacedOnly bool) ([]probe.Resource, error)
	RBAC(ctx context.Context, namespace string) (whocan.Snapshot, error)
}

// clusterFactory is overridable in tests.
var clusterFactory = func(kubeconfig, asUser string, asGroups []string) (cluster, error) {
	return kube.New(kubeconfig, asUser, asGroups)
}

// connFlags are the flags shared by every subcommand.
type connFlags struct {
	namespace  string
	asUser     string
	asGroups   []string
	kubeconfig string
}

func (f *connFlags) connect() (cluster, error) {
	return clusterFactory(f.kubeconfig, f.asUser, f.asGroups)
}

func newRootCmd() *cobra.Command {
	var (
		conn         connFlags
		verbs        []string
		resources    []string
		groups       []string
		allResources bool
		asJSON       bool
		noColor      bool
	)

	root := &cobra.Command{
		Use:   "kubectl-can",
		Short: "See what your current Kubernetes context can and cannot do",
		Long: "kubectl-can probes the current context's permissions with SelfSubjectAccessReview\n" +
			"and prints, grouped by resource, which verbs are allowed and denied.\n\n" +
			"`kubectl can who VERB RESOURCE` answers the reverse question: who is granted it.",
		Example: "  kubectl can --namespace prod\n" +
			"  kubectl can --verb delete --resource secrets --as alice\n" +
			"  kubectl can --all-resources --group argoproj.io,cert-manager.io",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := conn.connect()
			if err != nil {
				return err
			}
			ctx := context.Background()

			candidates := probe.DefaultResources
			if allResources {
				// A namespaced probe of a cluster-scoped resource asks a question
				// the authorizer answers for the whole cluster, so with
				// --namespace only namespaced resources are discovered.
				candidates, err = c.Resources(ctx, conn.namespace != "")
				if err != nil {
					if !errors.Is(err, probe.ErrPartialDiscovery) {
						return err
					}
					fmt.Fprintln(cmd.ErrOrStderr(), "kubectl-can: warning:", err)
				}
			}
			v, r := probe.Filter(probe.DefaultVerbs, candidates, verbs, resources)
			r = probe.FilterGroups(r, groups)

			results, err := probe.Probe(ctx, c, v, r, conn.namespace)
			if err != nil {
				return err
			}

			if asJSON {
				return report.JSON(cmd.OutOrStdout(), results)
			}
			report.Grouped(cmd.OutOrStdout(), results, !noColor, allResources)
			return nil
		},
	}

	pf := root.PersistentFlags()
	pf.StringVarP(&conn.namespace, "namespace", "n", "", "namespace to check (empty = cluster scope)")
	pf.StringVar(&conn.asUser, "as", "", "impersonate a user")
	pf.StringArrayVar(&conn.asGroups, "as-group", nil, "impersonate a group (repeatable)")
	pf.StringVar(&conn.kubeconfig, "kubeconfig", "", "path to kubeconfig")

	root.Flags().StringSliceVar(&verbs, "verb", nil, "only probe these verbs (comma-separated)")
	root.Flags().StringSliceVar(&resources, "resource", nil, "only probe these resources, by name or name.group (comma-separated)")
	root.Flags().StringSliceVar(&groups, "group", nil, "only probe resources in these API groups; \"core\" is the core group (comma-separated)")
	root.Flags().BoolVar(&allResources, "all-resources", false, "probe every resource the API server serves (discovery), CRDs included")
	root.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	root.Flags().BoolVar(&noColor, "no-color", false, "disable colored output")

	root.AddCommand(newWhoCmd(&conn))
	return root
}

func newWhoCmd(conn *connFlags) *cobra.Command {
	var (
		resourceName   string
		showEscalation bool
		output         string
	)
	cmd := &cobra.Command{
		Use:   "who VERB RESOURCE[.GROUP][/SUBRESOURCE]",
		Short: "List every subject that RBAC grants a verb on a resource",
		Long: "who reads ClusterRoles, ClusterRoleBindings and, with --namespace, that namespace's\n" +
			"Roles and RoleBindings, and lists every user, group and ServiceAccount they grant\n" +
			"VERB on RESOURCE to, with the bindings that grant it. Aggregated ClusterRoles,\n" +
			"wildcards, resourceNames and subresources are matched like the RBAC authorizer does.\n" +
			"Without --namespace only cluster-wide grants (ClusterRoleBindings) count.\n" +
			"Only RBAC is evaluated; webhook and Node authorizer grants are not visible.",
		Example: "  kubectl can who get secrets --namespace prod --show-escalation\n" +
			"  kubectl can who create pods/exec -n prod -o csv\n" +
			"  kubectl can who patch applications.argoproj.io -n argocd",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch output {
			case "text", "json", "csv":
			default:
				return fmt.Errorf("unknown --output %q (want text, json or csv)", output)
			}
			c, err := conn.connect()
			if err != nil {
				return err
			}
			ctx := context.Background()

			name, group, sub, hasGroup := whocan.ParseResource(args[1])
			if !hasGroup {
				group, err = resolveGroup(ctx, c, name)
				if err != nil {
					return err
				}
			}
			snap, err := c.RBAC(ctx, conn.namespace)
			if err != nil {
				return err
			}
			subjects := whocan.Lookup(snap, whocan.Request{
				Verb: args[0], Group: group, Resource: name, Subresource: sub,
				Name: resourceName, Namespace: conn.namespace,
			}, showEscalation)

			switch output {
			case "json":
				return report.SubjectsJSON(cmd.OutOrStdout(), subjects)
			case "csv":
				return report.SubjectsCSV(cmd.OutOrStdout(), subjects)
			}
			return report.Subjects(cmd.OutOrStdout(), subjects, showEscalation)
		},
	}
	cmd.Flags().StringVar(&resourceName, "resource-name", "", "check access to this object name only (matches rules with resourceNames)")
	cmd.Flags().BoolVar(&showEscalation, "show-escalation", false, "mark subjects that also hold bind, escalate, impersonate or secret read access")
	cmd.Flags().StringVarP(&output, "output", "o", "text", "output format: text, json or csv")
	return cmd
}

// resolveGroup finds the API group of an unqualified resource name: the
// built-in default list first, then discovery. A name served by more than one
// group must be qualified ("events.events.k8s.io").
func resolveGroup(ctx context.Context, c cluster, name string) (string, error) {
	for _, r := range probe.DefaultResources {
		if r.Name == name {
			return r.Group, nil
		}
	}
	all, err := c.Resources(ctx, false)
	if err != nil && !errors.Is(err, probe.ErrPartialDiscovery) {
		return "", err
	}
	var found []probe.Resource
	for _, r := range all {
		if r.Name == name {
			found = append(found, r)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("resource %q is not served by the cluster; qualify it as name.group if discovery is incomplete", name)
	case 1:
		return found[0].Group, nil
	}
	var qualified []string
	for _, r := range found {
		qualified = append(qualified, r.Qualified())
	}
	return "", fmt.Errorf("resource %q is served by several groups, pick one: %v", name, qualified)
}

// Execute runs the root command.
func Execute() error {
	return newRootCmd().Execute()
}

// RunWith lets tests drive the command with injected output.
func RunWith(args []string, out io.Writer) error {
	c := newRootCmd()
	c.SetOut(out)
	c.SetErr(out)
	c.SetArgs(args)
	return c.Execute()
}

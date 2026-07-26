// Package cmd wires the kubectl-can CLI (installable as `kubectl can` via krew).
package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/moveeeax/kubectl-can/internal/kube"
	"github.com/moveeeax/kubectl-can/internal/probe"
	"github.com/moveeeax/kubectl-can/internal/report"
)

// reviewerFactory is overridable in tests.
var reviewerFactory = func(kubeconfig, asUser string, asGroups []string) (probe.Reviewer, error) {
	return kube.New(kubeconfig, asUser, asGroups)
}

func newRootCmd() *cobra.Command {
	var (
		namespace  string
		asUser     string
		asGroups   []string
		verbs      []string
		resources  []string
		asJSON     bool
		noColor    bool
		kubeconfig string
	)

	root := &cobra.Command{
		Use:           "kubectl-can",
		Short:         "See what your current Kubernetes context can and cannot do",
		Long:          "kubectl-can probes the current context's permissions with SelfSubjectAccessReview\nand prints, grouped by resource, which verbs are allowed and denied.",
		Example:       "  kubectl can --namespace prod\n  kubectl can --verb delete --resource secrets --as alice",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, r := probe.Filter(probe.DefaultVerbs, probe.DefaultResources, verbs, resources)

			reviewer, err := reviewerFactory(kubeconfig, asUser, asGroups)
			if err != nil {
				return err
			}
			results, err := probe.Probe(context.Background(), reviewer, v, r, namespace)
			if err != nil {
				return err
			}

			if asJSON {
				return report.JSON(cmd.OutOrStdout(), results)
			}
			report.Grouped(cmd.OutOrStdout(), results, !noColor)
			return nil
		},
	}

	root.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace to check (empty = cluster scope)")
	root.Flags().StringVar(&asUser, "as", "", "impersonate a user")
	root.Flags().StringArrayVar(&asGroups, "as-group", nil, "impersonate a group (repeatable)")
	root.Flags().StringSliceVar(&verbs, "verb", nil, "only probe these verbs (comma-separated)")
	root.Flags().StringSliceVar(&resources, "resource", nil, "only probe these resources (comma-separated)")
	root.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	root.Flags().BoolVar(&noColor, "no-color", false, "disable colored output")
	root.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig")
	return root
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

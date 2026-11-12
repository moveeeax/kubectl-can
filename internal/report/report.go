// Package report renders access probe results grouped by resource.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/moveeeax/kubectl-can/internal/probe"
)

// Grouped writes results grouped by resource, one line per resource listing the
// allowed verbs (✔) and denied verbs (✗). With qualify, resources outside the
// core group are labeled kubectl-style ("deployments.apps"); without it they are
// labeled by plain name unless two groups serve the same name, in which case
// both are qualified so they are not merged into one line.
func Grouped(w io.Writer, results []probe.Result, color, qualify bool) {
	type key struct{ name, group string }
	byRes := map[key][]probe.Result{}
	groupsOf := map[string]map[string]bool{}
	var order []key
	for _, r := range results {
		k := key{r.Resource, r.Group}
		if _, ok := byRes[k]; !ok {
			order = append(order, k)
		}
		byRes[k] = append(byRes[k], r)
		if groupsOf[r.Resource] == nil {
			groupsOf[r.Resource] = map[string]bool{}
		}
		groupsOf[r.Resource][r.Group] = true
	}
	labels := map[key]string{}
	width := 24
	for _, k := range order {
		label := k.name
		if qualify || len(groupsOf[k.name]) > 1 {
			label = probe.Resource{Name: k.name, Group: k.group}.Qualified()
		}
		labels[k] = label
		width = max(width, len(label))
	}
	sort.Slice(order, func(i, j int) bool {
		if labels[order[i]] != labels[order[j]] {
			return labels[order[i]] < labels[order[j]]
		}
		return order[i].group < order[j].group
	})
	for _, k := range order {
		var parts []string
		for _, r := range byRes[k] {
			mark := "✗ " + r.Verb
			if r.Allowed {
				mark = "✔ " + r.Verb
			}
			if color {
				c := red
				if r.Allowed {
					c = green
				}
				mark = c + mark + reset
			}
			parts = append(parts, mark)
		}
		fmt.Fprintf(w, "%-*s %s\n", width, labels[k], strings.Join(parts, "  "))
	}
}

const (
	green = "\033[32m"
	red   = "\033[31m"
	reset = "\033[0m"
)

// JSON writes results as a JSON array.
func JSON(w io.Writer, results []probe.Result) error {
	if results == nil {
		results = []probe.Result{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

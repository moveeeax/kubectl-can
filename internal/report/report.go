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
// allowed verbs (✔) and denied verbs (✗).
func Grouped(w io.Writer, results []probe.Result, color bool) {
	byRes := map[string][]probe.Result{}
	var order []string
	for _, r := range results {
		if _, ok := byRes[r.Resource]; !ok {
			order = append(order, r.Resource)
		}
		byRes[r.Resource] = append(byRes[r.Resource], r)
	}
	sort.Strings(order)
	for _, res := range order {
		var parts []string
		for _, r := range byRes[res] {
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
		fmt.Fprintf(w, "%-24s %s\n", res, strings.Join(parts, "  "))
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

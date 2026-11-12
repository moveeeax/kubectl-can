package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/moveeeax/kubectl-can/internal/whocan"
)

// Subjects writes a who-can result as a table, one line per subject and
// binding. The ESCALATION column is shown only when showEscalation is set.
func Subjects(w io.Writer, subjects []whocan.Subject, showEscalation bool) error {
	if len(subjects) == 0 {
		_, err := fmt.Fprintln(w, "no subject is granted this by RBAC")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	header := "KIND\tSUBJECT\tBINDING\tROLE"
	if showEscalation {
		header += "\tESCALATION"
	}
	fmt.Fprintln(tw, header)
	for _, s := range subjects {
		for _, v := range s.Via {
			line := s.Kind + "\t" + subjectName(s) + "\t" + bindingName(v) + "\t" + v.RoleKind + "/" + v.RoleName
			if showEscalation {
				esc := strings.Join(s.Escalations, ",")
				if esc == "" {
					esc = "-"
				}
				line += "\t" + esc
			}
			fmt.Fprintln(tw, line)
		}
	}
	return tw.Flush()
}

// SubjectsJSON writes a who-can result as a JSON array of subjects.
func SubjectsJSON(w io.Writer, subjects []whocan.Subject) error {
	if subjects == nil {
		subjects = []whocan.Subject{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(subjects)
}

// SubjectsCSV writes a who-can result as CSV with a header row, one row per
// subject and binding. Escalations are joined with ";" in a single column.
func SubjectsCSV(w io.Writer, subjects []whocan.Subject) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"kind", "name", "namespace", "binding_kind", "binding_name", "binding_namespace", "role_kind", "role_name", "escalations"})
	for _, s := range subjects {
		for _, v := range s.Via {
			_ = cw.Write([]string{s.Kind, s.Name, s.Namespace, v.BindingKind, v.BindingName, v.BindingNamespace, v.RoleKind, v.RoleName, strings.Join(s.Escalations, ";")})
		}
	}
	cw.Flush()
	return cw.Error()
}

func subjectName(s whocan.Subject) string {
	if s.Namespace != "" {
		return s.Namespace + "/" + s.Name
	}
	return s.Name
}

func bindingName(v whocan.Via) string {
	if v.BindingNamespace != "" {
		return v.BindingKind + "/" + v.BindingNamespace + "/" + v.BindingName
	}
	return v.BindingKind + "/" + v.BindingName
}

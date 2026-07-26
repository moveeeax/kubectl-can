package main

import (
	"fmt"
	"os"

	"github.com/moveeeax/kubectl-can/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "kubectl-can:", err)
		os.Exit(1)
	}
}

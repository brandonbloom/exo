package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/deref/exo/exod/api"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(lsCmd)
}

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "Lists components",
	Long:  `Lists components.`,
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := newContext()
		ensureDaemon()
		cl := newClient()
		workspace := requireWorkspace(ctx, cl)
		output, err := workspace.DescribeComponents(ctx, &api.DescribeComponentsInput{})
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 4, 8, 3, ' ', 0)
		for _, component := range output.Components {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", component.Name, component.ID, component.Type)
		}
		_ = w.Flush()
		return nil
	},
}
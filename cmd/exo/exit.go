package main

import (
	"os"

	"github.com/deref/exo/util/cmdutil"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(exitCmd)
}

var exitCmd = &cobra.Command{
	Use:   "exit",
	Short: "Stop the exo daemon",
	Long:  `Stop the exo daemon process.`,
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		paths := cmdutil.MustMakeDirectories()
		loadRunState(paths.RunStateFile)
		if runState.Pid == 0 {
			return nil
		}

		killExod(paths)
		return nil
	},
}

func killExod(paths *cmdutil.KnownPaths) {
	process, err := os.FindProcess(runState.Pid)
	if err != nil {
		panic(err)
	}
	_ = process.Kill()

	// TODO: Wait for process to exit.

	if err := os.Remove(paths.RunStateFile); err != nil {
		cmdutil.Fatalf("removing run state file: %w", err)
	}
}

package commands

import "github.com/spf13/cobra"

func newScheduledCmd(_ *options) *cobra.Command {
	c := &cobra.Command{
		Use:   "scheduled",
		Short: "Tasks that run on their own in a workspace: a command or a prompt, on a schedule",
	}
	c.AddCommand(newScheduledLocalCmd())
	return c
}

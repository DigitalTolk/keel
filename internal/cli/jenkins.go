package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/DigitalTolk/keel/internal/jenkins"
)

func newJenkinsCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jenkins",
		Short: "Jenkins maintenance helpers",
	}
	cmd.AddCommand(newJenkinsBatchEditCmd(a))
	return cmd
}

func newJenkinsBatchEditCmd(a *app) *cobra.Command {
	var root, name string
	cmd := &cobra.Command{
		Use:   "batch-edit SEARCH REPLACE",
		Short: "Replace a literal string in every matching file (default config.xml) under a directory",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			changed, err := jenkins.BatchReplace(root, name, args[0], args[1])
			if err != nil {
				return err
			}
			for _, p := range changed {
				a.log.Info(fmt.Sprintf("edited %s", p))
			}
			a.log.Success(fmt.Sprintf("batch-edit complete: %d file(s) changed", len(changed)))
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", ".", "directory to search recursively")
	cmd.Flags().StringVar(&name, "name", "config.xml", "filename to match")
	return cmd
}

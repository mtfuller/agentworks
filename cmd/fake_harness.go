package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

var fakeHarnessMode string

var fakeHarnessCmd = &cobra.Command{
	Use:    "__fake-harness",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		prompt, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), 64<<10))
		if err != nil {
			return fmt.Errorf("read fake harness request: %w", err)
		}
		switch fakeHarnessMode {
		case "success":
			fmt.Fprintf(cmd.OutOrStdout(), "Fake harness completed a %d-byte request.\n", len(prompt))
			return nil
		case "slow":
			time.Sleep(350 * time.Millisecond)
			fmt.Fprintln(cmd.OutOrStdout(), "Fake harness completed after a delay.")
			return nil
		case "write":
			if err := os.WriteFile("agentworks-fake-output.txt", prompt, 0o600); err != nil {
				return fmt.Errorf("write fake harness output: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Fake harness wrote agentworks-fake-output.txt.")
			return nil
		case "fail":
			return fmt.Errorf("fake harness failure requested")
		case "hang":
			for {
				time.Sleep(time.Second)
			}
		case "descendants":
			child := exec.Command(os.Args[0], "__fake-harness", "--mode", "hang")
			child.Stdout = cmd.OutOrStdout()
			child.Stderr = cmd.ErrOrStderr()
			if err := child.Start(); err != nil {
				return fmt.Errorf("start fake harness descendant: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Fake harness spawned descendant %d.\n", child.Process.Pid)
			if err := child.Wait(); err != nil {
				return fmt.Errorf("wait for fake harness descendant: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("unknown fake harness mode %q", fakeHarnessMode)
		}
	},
}

func init() {
	fakeHarnessCmd.Flags().StringVar(&fakeHarnessMode, "mode", "success", "development harness behavior")
	rootCmd.AddCommand(fakeHarnessCmd)
}

package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/mtfuller/agentworks/internal/approval"
	"github.com/mtfuller/agentworks/internal/color"
	"github.com/mtfuller/agentworks/internal/connectors"
	"github.com/mtfuller/agentworks/internal/harness/claudecode"
	"github.com/mtfuller/agentworks/internal/harness/fake"
	"github.com/mtfuller/agentworks/internal/harness/githubcopilot"
	awprocess "github.com/mtfuller/agentworks/internal/process"
	"github.com/mtfuller/agentworks/internal/project"
	"github.com/mtfuller/agentworks/internal/router"
	"github.com/mtfuller/agentworks/internal/runtimepath"
	"github.com/mtfuller/agentworks/internal/scheduler"
	"github.com/mtfuller/agentworks/internal/spec"
	runtimestorage "github.com/mtfuller/agentworks/internal/storage"
	"github.com/mtfuller/agentworks/internal/store"
	"github.com/mtfuller/agentworks/internal/studio"
	"github.com/mtfuller/agentworks/internal/worker"
	awworkspace "github.com/mtfuller/agentworks/internal/workspace"
)

var (
	studioExperimental bool
	studioNoOpen       bool
	studioPort         int
	studioFakeHarness  bool
)

var studioCmd = &cobra.Command{
	Use:   "studio",
	Short: "Run the experimental localhost AgentWorks Studio",
	Long: `Start the format-2 Studio runtime in the foreground on a loopback-only random
port, serve its embedded web UI, and stop it cleanly on Ctrl-C. Readonly and write-capable
runs execute through installed Claude Code and GitHub Copilot CLIs; mutating actions use
bounded, run-only Studio approvals. An explicit development flag also enables the
deterministic fake harness.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !studioExperimental {
			return fmt.Errorf("Studio is not ready for normal use; pass --experimental to run the current localhost runtime")
		}
		root, err := project.FindRoot(projectFlag)
		if err != nil {
			return fmt.Errorf("%w (pass --project with a format-2 project)", err)
		}
		manifest, err := spec.LoadProject(root)
		if err != nil {
			return err
		}
		if studioPort < 0 || studioPort > 65535 {
			return fmt.Errorf("Studio port %d is outside 0-65535", studioPort)
		}
		databasePath, err := runtimepath.DatabasePath(root)
		if err != nil {
			return err
		}
		logRoot, err := runtimepath.LogRoot(root)
		if err != nil {
			return err
		}
		storageLimit, err := spec.ParseByteSize(manifest.Runtime.StorageLimit)
		if err != nil {
			return fmt.Errorf("parse runtime storage limit: %w", err)
		}

		ctx, cancel := studioSignalContext(cmd.Context())
		defer cancel()
		runtimeStore, err := store.Open(ctx, databasePath, store.Options{})
		if err != nil {
			return err
		}
		defer runtimeStore.Close()
		approvalGateway, err := approval.StartGateway(ctx, runtimeStore, 0)
		if err != nil {
			return err
		}
		harnesses := []string{"claude-code", "github-copilot"}
		workspaceRegistry := awworkspace.NewRegistry()
		adapters := map[string]worker.Adapter{
			"claude-code":    claudecode.ForProject(root),
			"github-copilot": githubcopilot.ForProject(root),
		}
		if studioFakeHarness {
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate AgentWorks executable: %w", err)
			}
			adapters["fake"] = fake.Adapter{Executable: executable}
			harnesses = append([]string{"fake"}, harnesses...)
			color.Warning("Development fake harness is enabled; prompts can use [fake:fail], [fake:hang], [fake:slow], [fake:write], [fake:command], [fake:retry-once], or [fake:descendants].")
		}
		runWorker, err := worker.New(worker.Config{
			Store: runtimeStore, Owner: fmt.Sprintf("studio:%d", os.Getpid()), LogRoot: logRoot,
			ProjectRoot: root,
			ResolveWorkspace: func(alias string) (string, error) {
				if binding, ok := workspaceRegistry.Resolve(alias); ok {
					return binding.Path, nil
				}
				local, err := spec.LoadLocal(root)
				if err != nil {
					return "", err
				}
				binding, ok := local.Workspaces[alias]
				if !ok {
					return "", fmt.Errorf("workspace %q has no local binding", alias)
				}
				return binding.Path, nil
			},
			Adapters: adapters, ApprovalGateway: approvalGateway,
		})
		if err != nil {
			return err
		}
		controller := worker.StartRuntime(ctx, runWorker)
		eventService := &router.Service{Root: root, Store: runtimeStore, Harnesses: harnesses, Wake: controller.Wake}
		scheduleRuntime := scheduler.Start(ctx, root, runtimeStore, eventService)
		connectorService := &connectors.Service{Root: root, Store: runtimeStore, Router: eventService}
		connectorRuntime := connectors.Start(ctx, connectorService)
		storageManager := &runtimestorage.Manager{Store: runtimeStore, LogRoot: logRoot, Limit: storageLimit}
		storageRuntime := runtimestorage.Start(ctx, *storageManager)
		instance, err := studio.Start(ctx, studio.Config{
			Host: "127.0.0.1", Port: studioPort, ProjectName: manifest.Name,
			ProjectRoot: root, Store: runtimeStore, Harnesses: harnesses,
			Controller: controller, LogRoot: logRoot, Workspaces: workspaceRegistry,
			StorageLimit: storageLimit,
			Events:       eventService,
			Connectors:   connectorService,
			Storage:      storageManager,
		})
		if err != nil {
			return err
		}
		color.Success("AgentWorks Studio is running at %s", instance.URL())
		color.Info("Press Ctrl-C to stop Studio and cancel active work.")
		if !studioNoOpen {
			if err := openStudioBrowser(ctx, instance.URL()); err != nil {
				color.Warning("Could not open a browser automatically: %v", err)
			}
		}
		serverErr := instance.Wait()
		cancel()
		controller.Wait()
		scheduleRuntime.Wait()
		connectorRuntime.Wait()
		storageRuntime.Wait()
		return serverErr
	},
}

func openStudioBrowser(parent context.Context, url string) error {
	var executable string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		executable, args = "open", []string{url}
	case "windows":
		executable, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "linux":
		executable, args = "xdg-open", []string{url}
	default:
		return fmt.Errorf("automatic browser opening is unsupported on %s", runtime.GOOS)
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	_, err := awprocess.Run(ctx, awprocess.Spec{
		Executable: executable, Args: args, Env: os.Environ(), GracePeriod: time.Second,
	})
	return err
}

func init() {
	studioCmd.Flags().BoolVar(&studioExperimental, "experimental", false, "acknowledge that Studio's runtime APIs are still under development")
	studioCmd.Flags().BoolVar(&studioNoOpen, "no-open", false, "do not open Studio in the default browser")
	studioCmd.Flags().IntVar(&studioPort, "port", 0, "loopback port (0 selects an available port)")
	studioCmd.Flags().BoolVar(&studioFakeHarness, "fake-harness", false, "enable the development-only local fake harness")
	rootCmd.AddCommand(studioCmd)
}

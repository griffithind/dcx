package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	debugOutputJSON bool
)

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Show detailed debugging information",
	Long: `Display comprehensive debugging information for troubleshooting.

This command shows:
- Docker environment information
- Resolved configuration details
- Computed hashes for staleness detection
- Feature resolution order
- Container state and labels
- Staleness analysis

Useful for debugging issues with devcontainer configuration or
understanding why a container is being recreated.

Examples:
  dcx debug              # Show debug info for current directory
  dcx debug --json       # Output as JSON for analysis
  dcx debug -p /path     # Debug specific workspace`,
	RunE: runDebug,
}

func init() {
	debugCmd.Flags().BoolVar(&debugOutputJSON, "json", false, "output debug info as JSON")
	debugCmd.Hidden = true
	rootCmd.AddCommand(debugCmd)
}

// DebugInfo contains all debugging information.
type DebugInfo struct {
	Version   string         `json:"version"`
	Platform  PlatformInfo   `json:"platform"`
	Docker    DockerInfo     `json:"docker"`
	Workspace WorkspaceDebug `json:"workspace"`
	Config    ConfigDebug    `json:"config"`
	Container ContainerDebug `json:"container"`
	Staleness StalenessDebug `json:"staleness"`
}

type PlatformInfo struct {
	OS    string `json:"os"`
	Arch  string `json:"arch"`
	GoVer string `json:"go_version"`
}

type DockerInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version,omitempty"`
	APIVersion   string `json:"api_version,omitempty"`
	BuildVersion string `json:"build_version,omitempty"`
	Error        string `json:"error,omitempty"`
}

type WorkspaceDebug struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	ConfigPath   string `json:"config_path"`
	PlanType     string `json:"plan_type"`
	Image        string `json:"image,omitempty"`
	FinalImage   string `json:"final_image,omitempty"`
	ServiceName  string `json:"service_name"`
	FeatureCount int    `json:"feature_count"`
}

type ConfigDebug struct {
	HasImage      bool        `json:"has_image"`
	HasDockerfile bool        `json:"has_dockerfile"`
	HasCompose    bool        `json:"has_compose"`
	Features      []string    `json:"features,omitempty"`
	Hashes        HashesDebug `json:"hashes"`
}

type HashesDebug struct {
	Config     string `json:"config"`
	Dockerfile string `json:"dockerfile,omitempty"`
	Compose    string `json:"compose,omitempty"`
	Features   string `json:"features,omitempty"`
	Overall    string `json:"overall"`
}

type ContainerDebug struct {
	Found        bool              `json:"found"`
	ID           string            `json:"id,omitempty"`
	Name         string            `json:"name,omitempty"`
	State        string            `json:"state,omitempty"`
	Running      bool              `json:"running,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	LegacyLabels bool              `json:"legacy_labels,omitempty"`
}

type StalenessDebug struct {
	IsStale bool     `json:"is_stale"`
	Reason  string   `json:"reason,omitempty"`
	Changes []string `json:"changes,omitempty"`
}

func runDebug(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	debug := &DebugInfo{
		Version: "dev",
		Platform: PlatformInfo{
			OS:    runtime.GOOS,
			Arch:  runtime.GOARCH,
			GoVer: runtime.Version(),
		},
	}

	// Docker info
	debug.Docker = getDockerInfo(ctx)

	// Try to parse configuration
	if err := populateConfigDebug(ctx, debug); err != nil {
		debug.Config = ConfigDebug{}
	}

	// Get container info if Docker is available
	if debug.Docker.Available {
		populateContainerDebug(ctx, debug)
	}

	if debugOutputJSON {
		return outputDebugJSON(debug)
	}

	return outputDebugTable(debug)
}

func getDockerInfo(ctx context.Context) DockerInfo {
	info := DockerInfo{}

	client, err := container.NewDockerClient()
	if err != nil {
		info.Available = false
		info.Error = err.Error()
		return info
	}
	defer func() { _ = client.Close() }()

	version, err := client.ServerVersion(ctx)
	if err != nil {
		info.Available = false
		info.Error = err.Error()
		return info
	}

	info.Available = true
	info.Version = version

	return info
}

func populateConfigDebug(ctx context.Context, debug *DebugInfo) error {
	// Find config path
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = findConfigPath(workspacePath)
		if cfgPath == "" {
			return fmt.Errorf("config not found")
		}
	}

	// Parse configuration
	cfg, err := devcontainer.ParseFile(cfgPath)
	if err != nil {
		return err
	}

	// Build resolved devcontainer
	builder := devcontainer.NewBuilder(nil)
	resolved, err := builder.Build(ctx, devcontainer.BuilderOptions{
		ConfigPath:    cfgPath,
		WorkspaceRoot: workspacePath,
		Config:        cfg,
	})
	if err != nil {
		return err
	}

	// Populate workspace debug
	planType := ""
	if resolved.Plan != nil {
		planType = string(resolved.Plan.Type())
	}
	debug.Workspace = WorkspaceDebug{
		ID:           resolved.ID,
		Name:         resolved.Name,
		Path:         resolved.LocalRoot,
		ConfigPath:   resolved.ConfigPath,
		PlanType:     planType,
		Image:        resolved.BaseImage,
		FinalImage:   resolved.Image,
		ServiceName:  resolved.ServiceName,
		FeatureCount: len(resolved.Features),
	}

	// Populate config debug
	debug.Config = ConfigDebug{
		HasImage:      cfg.Image != "",
		HasDockerfile: cfg.Build != nil,
		HasCompose:    cfg.IsComposePlan(),
		Hashes: HashesDebug{
			Config:     resolved.Hashes.Config,
			Dockerfile: resolved.Hashes.Dockerfile,
			Compose:    resolved.Hashes.Compose,
			Features:   resolved.Hashes.Features,
			Overall:    resolved.Hashes.Overall,
		},
	}

	// Extract feature IDs
	for featureRef := range cfg.Features {
		debug.Config.Features = append(debug.Config.Features, featureRef)
	}

	return nil
}

func populateContainerDebug(ctx context.Context, debug *DebugInfo) {
	client, err := container.NewDockerClient()
	if err != nil {
		return
	}
	defer func() { _ = client.Close() }()

	svc := service.NewDevContainerService(client, workspacePath, configPath, verbose)
	defer svc.Close()

	ids, err := svc.GetIdentifiers()
	if err != nil {
		return
	}

	// Try to find container by workspace ID
	workspaceID := debug.Workspace.ID
	if workspaceID == "" {
		workspaceID = ids.WorkspaceID
	}

	s, info, _ := svc.GetStateManager().GetStateWithProject(ctx, ids.ProjectName, workspaceID)

	debug.Container = ContainerDebug{
		Found: info != nil,
	}

	if info != nil {
		debug.Container.ID = info.ID
		debug.Container.Name = info.Name
		debug.Container.State = string(s)
		debug.Container.Running = info.Running

		// Get labels
		debug.Container.Labels = info.Labels.ToMap()
		debug.Container.LegacyLabels = info.Labels.SchemaVersion == "" || info.Labels.SchemaVersion == "1"

		// Check staleness
		if debug.Config.Hashes.Overall != "" {
			if info.Labels.HashConfig != debug.Config.Hashes.Config {
				debug.Staleness.IsStale = true
				debug.Staleness.Reason = "Configuration changed"
				if info.Labels.HashConfig != "" {
					debug.Staleness.Changes = append(debug.Staleness.Changes,
						fmt.Sprintf("config hash: %s -> %s", info.Labels.HashConfig, debug.Config.Hashes.Config))
				}
			}
		}
	} else {
		debug.Staleness.IsStale = true
		debug.Staleness.Reason = "No container found"
	}
}

func outputDebugJSON(debug *DebugInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(debug)
}

func outputDebugTable(debug *DebugInfo) error {
	ui.Println("DCX Debug Information")
	ui.Println("=====================")
	ui.Println("")

	// Platform
	ui.Println("Platform:")
	ui.Printf("  OS:         %s/%s", debug.Platform.OS, debug.Platform.Arch)
	ui.Printf("  Go Version: %s", debug.Platform.GoVer)
	ui.Printf("  DCX Version: %s", debug.Version)
	ui.Println("")

	// Docker
	ui.Println("Docker:")
	if debug.Docker.Available {
		ui.Printf("  Status:      Available")
		ui.Printf("  Version:     %s", debug.Docker.Version)
		ui.Printf("  API Version: %s", debug.Docker.APIVersion)
	} else {
		ui.Printf("  Status:  Not Available")
		ui.Printf("  Error:   %s", debug.Docker.Error)
	}
	ui.Println("")

	// Workspace
	if debug.Workspace.ID != "" {
		ui.Println("Workspace:")
		ui.Printf("  ID:           %s", debug.Workspace.ID)
		ui.Printf("  Name:         %s", debug.Workspace.Name)
		ui.Printf("  Path:         %s", debug.Workspace.Path)
		ui.Printf("  Config:       %s", debug.Workspace.ConfigPath)
		ui.Printf("  Plan Type:    %s", debug.Workspace.PlanType)
		if debug.Workspace.Image != "" {
			ui.Printf("  Image:        %s", debug.Workspace.Image)
		}
		ui.Printf("  Service Name: %s", debug.Workspace.ServiceName)
		ui.Printf("  Features:     %d", debug.Workspace.FeatureCount)
		ui.Println("")
	}

	// Configuration
	if debug.Config.Hashes.Config != "" {
		ui.Println("Configuration:")
		ui.Printf("  Has Image:      %v", debug.Config.HasImage)
		ui.Printf("  Has Dockerfile: %v", debug.Config.HasDockerfile)
		ui.Printf("  Has Compose:    %v", debug.Config.HasCompose)
		if len(debug.Config.Features) > 0 {
			ui.Printf("  Features:")
			for _, f := range debug.Config.Features {
				ui.Printf("    - %s", f)
			}
		}
		ui.Println("")

		ui.Println("Hashes:")
		ui.Printf("  Config:     %s", debug.Config.Hashes.Config)
		if debug.Config.Hashes.Dockerfile != "" {
			ui.Printf("  Dockerfile: %s", debug.Config.Hashes.Dockerfile)
		}
		if debug.Config.Hashes.Compose != "" {
			ui.Printf("  Compose:    %s", debug.Config.Hashes.Compose)
		}
		if debug.Config.Hashes.Features != "" {
			ui.Printf("  Features:   %s", debug.Config.Hashes.Features)
		}
		ui.Printf("  Overall:    %s", debug.Config.Hashes.Overall)
		ui.Println("")
	}

	// Container
	ui.Println("Container:")
	if debug.Container.Found {
		ui.Printf("  ID:      %s", debug.Container.ID[:12])
		ui.Printf("  Name:    %s", debug.Container.Name)
		ui.Printf("  State:   %s", debug.Container.State)
		ui.Printf("  Running: %v", debug.Container.Running)
		if debug.Container.LegacyLabels {
			ui.Printf("  Labels:  Using legacy format (io.github.dcx)")
		} else {
			ui.Printf("  Labels:  Using new format (com.griffithind.dcx)")
		}
		ui.Println("")

		// Show key labels
		if len(debug.Container.Labels) > 0 {
			ui.Println("Key Labels:")
			for k, v := range debug.Container.Labels {
				// Only show dcx labels
				if strings.Contains(k, "dcx") {
					// Truncate long values
					if len(v) > 50 {
						v = v[:47] + "..."
					}
					ui.Printf("  %s: %s", k, v)
				}
			}
			ui.Println("")
		}
	} else {
		ui.Printf("  Status: Not found")
		ui.Println("")
	}

	// Staleness
	ui.Println("Staleness:")
	if debug.Staleness.IsStale {
		ui.Printf("  Status: Stale")
		ui.Printf("  Reason: %s", debug.Staleness.Reason)
		if len(debug.Staleness.Changes) > 0 {
			ui.Println("  Changes:")
			for _, c := range debug.Staleness.Changes {
				ui.Printf("    - %s", c)
			}
		}
	} else {
		ui.Printf("  Status: Up to date")
	}
	ui.Println("")

	// Suggestions
	if debug.Staleness.IsStale {
		ui.Println("Suggestions:")
		if !debug.Container.Found {
			ui.Println("  Run 'dcx up' to create the container")
		} else {
			ui.Println("  Run 'dcx up --recreate' to update the container")
		}
		ui.Println("")
	}

	// Label migration notice
	if debug.Container.Found && debug.Container.LegacyLabels {
		ui.Println("Note: Container uses legacy labels.")
		ui.Println("Run 'dcx up --recreate' to migrate to new label format.")
		ui.Println("")
	}

	return nil
}

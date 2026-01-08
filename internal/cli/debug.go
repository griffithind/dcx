package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
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
	Version     string           `json:"version"`
	Platform    PlatformInfo     `json:"platform"`
	Docker      DockerInfo       `json:"docker"`
	Workspace   WorkspaceDebug   `json:"workspace"`
	Config      ConfigDebug      `json:"config"`
	Container   ContainerDebug   `json:"container"`
	Staleness   StalenessDebug   `json:"staleness"`
}

type PlatformInfo struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	GoVer   string `json:"go_version"`
}

type DockerInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version,omitempty"`
	APIVersion   string `json:"api_version,omitempty"`
	BuildVersion string `json:"build_version,omitempty"`
	Error        string `json:"error,omitempty"`
}

type WorkspaceDebug struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	ConfigPath    string `json:"config_path"`
	PlanType      string `json:"plan_type"`
	Image         string `json:"image,omitempty"`
	FinalImage    string `json:"final_image,omitempty"`
	ServiceName   string `json:"service_name"`
	FeatureCount  int    `json:"feature_count"`
}

type ConfigDebug struct {
	HasImage      bool     `json:"has_image"`
	HasDockerfile bool     `json:"has_dockerfile"`
	HasCompose    bool     `json:"has_compose"`
	Features      []string `json:"features,omitempty"`
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
	Found         bool              `json:"found"`
	ID            string            `json:"id,omitempty"`
	Name          string            `json:"name,omitempty"`
	State         string            `json:"state,omitempty"`
	Running       bool              `json:"running,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	LegacyLabels  bool              `json:"legacy_labels,omitempty"`
}

type StalenessDebug struct {
	IsStale   bool     `json:"is_stale"`
	Reason    string   `json:"reason,omitempty"`
	Changes   []string `json:"changes,omitempty"`
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

	client, err := docker.NewClient()
	if err != nil {
		info.Available = false
		info.Error = err.Error()
		return info
	}
	defer client.Close()

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
	cfg, err := config.ParseFile(cfgPath)
	if err != nil {
		return err
	}

	// Build workspace
	builder := workspace.NewBuilder(nil)
	ws, err := builder.Build(ctx, workspace.BuildOptions{
		ConfigPath:    cfgPath,
		WorkspaceRoot: workspacePath,
		Config:        cfg,
	})
	if err != nil {
		return err
	}

	// Populate workspace debug
	debug.Workspace = WorkspaceDebug{
		ID:           ws.ID,
		Name:         ws.Name,
		Path:         ws.LocalRoot,
		ConfigPath:   ws.ConfigPath,
		PlanType:     string(ws.Resolved.PlanType),
		Image:        ws.Resolved.Image,
		FinalImage:   ws.Resolved.FinalImage,
		ServiceName:  ws.Resolved.ServiceName,
		FeatureCount: len(ws.Resolved.Features),
	}

	// Populate config debug
	debug.Config = ConfigDebug{
		HasImage:      cfg.Image != "",
		HasDockerfile: cfg.Build != nil,
		HasCompose:    cfg.IsComposePlan(),
		Hashes: HashesDebug{
			Config:     ws.Hashes.Config,
			Dockerfile: ws.Hashes.Dockerfile,
			Compose:    ws.Hashes.Compose,
			Features:   ws.Hashes.Features,
			Overall:    ws.Hashes.Overall,
		},
	}

	// Extract feature IDs
	for featureRef := range cfg.Features {
		debug.Config.Features = append(debug.Config.Features, featureRef)
	}

	return nil
}

func populateContainerDebug(ctx context.Context, debug *DebugInfo) {
	client, err := docker.NewClient()
	if err != nil {
		return
	}
	defer client.Close()

	stateMgr := state.NewManager(client)

	// Try to find container by workspace ID
	envKey := debug.Workspace.ID
	if envKey == "" {
		envKey = workspace.ComputeID(workspacePath)
	}

	s, info, _ := stateMgr.GetState(ctx, envKey)

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
	fmt.Println("DCX Debug Information")
	fmt.Println("=====================")
	fmt.Println()

	// Platform
	fmt.Println("Platform:")
	fmt.Printf("  OS:         %s/%s\n", debug.Platform.OS, debug.Platform.Arch)
	fmt.Printf("  Go Version: %s\n", debug.Platform.GoVer)
	fmt.Printf("  DCX Version: %s\n", debug.Version)
	fmt.Println()

	// Docker
	fmt.Println("Docker:")
	if debug.Docker.Available {
		fmt.Printf("  Status:      Available\n")
		fmt.Printf("  Version:     %s\n", debug.Docker.Version)
		fmt.Printf("  API Version: %s\n", debug.Docker.APIVersion)
	} else {
		fmt.Printf("  Status:  Not Available\n")
		fmt.Printf("  Error:   %s\n", debug.Docker.Error)
	}
	fmt.Println()

	// Workspace
	if debug.Workspace.ID != "" {
		fmt.Println("Workspace:")
		fmt.Printf("  ID:           %s\n", debug.Workspace.ID)
		fmt.Printf("  Name:         %s\n", debug.Workspace.Name)
		fmt.Printf("  Path:         %s\n", debug.Workspace.Path)
		fmt.Printf("  Config:       %s\n", debug.Workspace.ConfigPath)
		fmt.Printf("  Plan Type:    %s\n", debug.Workspace.PlanType)
		if debug.Workspace.Image != "" {
			fmt.Printf("  Image:        %s\n", debug.Workspace.Image)
		}
		fmt.Printf("  Service Name: %s\n", debug.Workspace.ServiceName)
		fmt.Printf("  Features:     %d\n", debug.Workspace.FeatureCount)
		fmt.Println()
	}

	// Configuration
	if debug.Config.Hashes.Config != "" {
		fmt.Println("Configuration:")
		fmt.Printf("  Has Image:      %v\n", debug.Config.HasImage)
		fmt.Printf("  Has Dockerfile: %v\n", debug.Config.HasDockerfile)
		fmt.Printf("  Has Compose:    %v\n", debug.Config.HasCompose)
		if len(debug.Config.Features) > 0 {
			fmt.Printf("  Features:\n")
			for _, f := range debug.Config.Features {
				fmt.Printf("    - %s\n", f)
			}
		}
		fmt.Println()

		fmt.Println("Hashes:")
		fmt.Printf("  Config:     %s\n", debug.Config.Hashes.Config)
		if debug.Config.Hashes.Dockerfile != "" {
			fmt.Printf("  Dockerfile: %s\n", debug.Config.Hashes.Dockerfile)
		}
		if debug.Config.Hashes.Compose != "" {
			fmt.Printf("  Compose:    %s\n", debug.Config.Hashes.Compose)
		}
		if debug.Config.Hashes.Features != "" {
			fmt.Printf("  Features:   %s\n", debug.Config.Hashes.Features)
		}
		fmt.Printf("  Overall:    %s\n", debug.Config.Hashes.Overall)
		fmt.Println()
	}

	// Container
	fmt.Println("Container:")
	if debug.Container.Found {
		fmt.Printf("  ID:      %s\n", debug.Container.ID[:12])
		fmt.Printf("  Name:    %s\n", debug.Container.Name)
		fmt.Printf("  State:   %s\n", debug.Container.State)
		fmt.Printf("  Running: %v\n", debug.Container.Running)
		if debug.Container.LegacyLabels {
			fmt.Printf("  Labels:  Using legacy format (io.github.dcx)\n")
		} else {
			fmt.Printf("  Labels:  Using new format (com.griffithind.dcx)\n")
		}
		fmt.Println()

		// Show key labels
		if len(debug.Container.Labels) > 0 {
			fmt.Println("Key Labels:")
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for k, v := range debug.Container.Labels {
				// Only show dcx labels
				if strings.Contains(k, "dcx") {
					// Truncate long values
					if len(v) > 50 {
						v = v[:47] + "..."
					}
					fmt.Fprintf(w, "  %s\t%s\n", k, v)
				}
			}
			w.Flush()
			fmt.Println()
		}
	} else {
		fmt.Printf("  Status: Not found\n")
		fmt.Println()
	}

	// Staleness
	fmt.Println("Staleness:")
	if debug.Staleness.IsStale {
		fmt.Printf("  Status: Stale\n")
		fmt.Printf("  Reason: %s\n", debug.Staleness.Reason)
		if len(debug.Staleness.Changes) > 0 {
			fmt.Println("  Changes:")
			for _, c := range debug.Staleness.Changes {
				fmt.Printf("    - %s\n", c)
			}
		}
	} else {
		fmt.Printf("  Status: Up to date\n")
	}
	fmt.Println()

	// Suggestions
	if debug.Staleness.IsStale {
		fmt.Println("Suggestions:")
		if !debug.Container.Found {
			fmt.Println("  Run 'dcx up' to create the container")
		} else {
			fmt.Println("  Run 'dcx up --recreate' to update the container")
		}
		fmt.Println()
	}

	// Label migration notice
	if debug.Container.Found && debug.Container.LegacyLabels {
		fmt.Println("Note: Container uses legacy labels.")
		fmt.Println("Run 'dcx up --recreate' to migrate to new label format.")
		fmt.Println()
	}

	return nil
}

// Ensure labels package is imported
var _ = labels.Prefix

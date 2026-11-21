package container

import (
	"context"
	"io"
	"time"

	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/state"
)

// Engine is the container engine dcx drives. The Docker CLI implementation
// (*Docker) is the only production engine; tests substitute fakes.
//
// It covers everything dcx asks of the engine; the CLI gets one from
// context.go and never builds a client itself.
type Engine interface {
	state.ContainerClient
	InspectContainer(ctx context.Context, containerID string) (*state.ContainerDetails, error)
	StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error
	RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error
	RemoveVolume(ctx context.Context, name string) error

	Info(ctx context.Context) (*SystemInfo, error)

	CreateContainer(ctx context.Context, spec ContainerSpec) (string, error)
	StartContainer(ctx context.Context, containerID string) error
	PortMapping(ctx context.Context, containerName string, containerPort int, proto string) (int, error)
	Logs(ctx context.Context, containerID string, opts LogsOptions) (io.ReadCloser, error)

	ImageExists(ctx context.Context, imageRef string) (bool, error)
	ImageID(ctx context.Context, imageRef string) (string, error)
	ImageLabels(ctx context.Context, imageRef string) (map[string]string, error)
	ImageConfig(ctx context.Context, imageRef string) (build.ImageConfig, error)
	PullImageWithProgress(ctx context.Context, imageRef string, progressOut io.Writer) error
	BuildImage(ctx context.Context, opts build.DockerfileBuildOptions) error

	Compose(ctx context.Context, project ComposeProject, args ...string) error
	ComposeVersion(ctx context.Context) (string, error)
	ComposeOutput(ctx context.Context, project ComposeProject, args ...string) ([]byte, error)

	ExecInContainer(ctx context.Context, containerName string, argv []string) error
	ExecOutput(ctx context.Context, containerName string, argv []string) ([]byte, error)
	ExecAsRoot(ctx context.Context, containerName string, argv []string) ([]byte, error)
	CopyToContainer(ctx context.Context, src, containerName, dest string) error
	ExecAsRootWithInput(ctx context.Context, containerName string, argv []string, input []byte) ([]byte, error)

	// Image maintenance (dcx clean).
	CleanupDerivedImages(ctx context.Context, workspaceID string) (*CleanupResult, error)
	CleanupDanglingImages(ctx context.Context) (*CleanupResult, error)
	DerivedImageStats(ctx context.Context) (count int, totalSize int64, err error)
}

var _ Engine = (*Docker)(nil)

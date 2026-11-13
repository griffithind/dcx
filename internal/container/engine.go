package container

import (
	"context"
	"io"

	"github.com/griffithind/dcx/internal/state"
)

// Engine is the container engine dcx drives. The Docker CLI implementation
// (*Docker) is the only production engine; tests substitute fakes.
//
// It covers what the service, runtime and secret code need. Inventory and
// maintenance commands (clean, list) use *Docker directly.
type Engine interface {
	state.ContainerClient

	Info(ctx context.Context) (*SystemInfo, error)

	CreateContainer(ctx context.Context, spec ContainerSpec) (string, error)
	StartContainer(ctx context.Context, containerID string) error
	PortMapping(ctx context.Context, containerName string, containerPort int, proto string) (int, error)
	GetLogs(ctx context.Context, containerID string, opts LogsOptions) (io.ReadCloser, error)

	ImageExists(ctx context.Context, imageRef string) (bool, error)
	GetImageID(ctx context.Context, imageRef string) (string, error)
	GetImageLabels(ctx context.Context, imageRef string) (map[string]string, error)
	PullImageWithProgress(ctx context.Context, imageRef string, progressOut io.Writer) error

	ExecInContainer(ctx context.Context, containerName string, argv []string) error
	ExecDetached(ctx context.Context, containerName string, argv []string) error
	ExecOutput(ctx context.Context, containerName string, argv []string) ([]byte, error)
	ExecAsRoot(ctx context.Context, containerName string, argv []string) ([]byte, error)
	MkdirInContainer(ctx context.Context, containerName, path, user string) error
	ChmodInContainer(ctx context.Context, containerName, path, mode, user string) error
	ChownInContainer(ctx context.Context, containerName, path, owner string) error
	WriteFileInContainer(ctx context.Context, containerName, path string, content []byte, user string) error
}

var _ Engine = (*Docker)(nil)

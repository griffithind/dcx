// Package containertest provides an in-memory container.Engine for tests.
package containertest

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/state"
)

// Call records one engine invocation.
type Call struct {
	Method string
	Args   []string
}

func (c Call) String() string { return c.Method + "(" + strings.Join(c.Args, ", ") + ")" }

// Fake is an in-memory container.Engine. Zero value is ready to use; set
// fields to shape responses. All methods are safe for concurrent use.
type Fake struct {
	mu sync.Mutex

	// Containers is what ListContainersWithLabels filters over.
	Containers []state.ContainerSummary
	// Ports maps "<container>/<port>" to the published host port.
	Ports map[string]int
	// Images lists image refs that exist locally.
	Images map[string]bool
	// LocalImageLabels maps image refs to their labels (local images).
	LocalImageLabels map[string]map[string]string
	// ImageConfigs maps image refs to their configuration.
	ImageConfigs map[string]build.ImageConfig
	// Registry maps image refs to the labels they get once pulled.
	Registry map[string]map[string]string
	// ExecFunc decides the result of ExecInContainer (nil = success).
	ExecFunc func(containerName string, argv []string) error
	// Outputs maps a space-joined argv to ExecOutput's result.
	Outputs map[string]string
	// OutputFunc answers ExecOutput calls Outputs does not match.
	OutputFunc func(containerName string, argv []string) ([]byte, error)
	// Files records WriteFileInContainer content by "<container>:<path>".
	Files map[string][]byte
	// ComposeOutputs maps space-joined compose arguments (after the
	// project flags) to ComposeOutput's result.
	ComposeOutputs map[string]string
	// Builds records every BuildImage request.
	Builds []build.DockerfileBuildOptions
	// Specs records every CreateContainer request.
	Specs []container.ContainerSpec
	// Env holds each container's environment (KEY=VALUE) by ID or name.
	Env map[string][]string
	// StartedAt holds each container's start time (RFC 3339) by ID.
	StartedAt map[string]string
	// Volumes holds each container's named volumes by ID.
	Volumes map[string][]string
	// SELinux is what Info reports for the daemon's SELinux labelling.
	SELinux bool

	calls   []Call
	created int
}

var _ container.Engine = (*Fake)(nil)

func (f *Fake) record(method string, args ...string) {
	f.calls = append(f.calls, Call{Method: method, Args: args})
}

// Calls returns a copy of all recorded calls.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// CallsTo returns the recorded calls for one method.
func (f *Fake) CallsTo(method string) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func (f *Fake) ListContainersWithLabels(_ context.Context, labels map[string]string) ([]state.ContainerSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ListContainersWithLabels")
	var out []state.ContainerSummary
	for _, c := range f.Containers {
		match := true
		for k, v := range labels {
			if c.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *Fake) find(id string) *state.ContainerSummary {
	for i := range f.Containers {
		if f.Containers[i].ID == id || f.Containers[i].Name == id {
			return &f.Containers[i]
		}
	}
	return nil
}

func (f *Fake) InspectContainer(_ context.Context, id string) (*state.ContainerDetails, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("InspectContainer", id)
	c := f.find(id)
	if c == nil {
		return nil, fmt.Errorf("container not found: %s", id)
	}
	env := f.Env[c.ID]
	if env == nil {
		env = f.Env[c.Name]
	}
	return &state.ContainerDetails{ID: c.ID, Name: c.Name, State: c.State, Running: c.Running, Labels: c.Labels, Env: env, StartedAt: f.StartedAt[c.ID], Volumes: f.Volumes[c.ID]}, nil
}

func (f *Fake) StopContainer(_ context.Context, id string, _ *time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("StopContainer", id)
	if c := f.find(id); c != nil {
		c.Running, c.State = false, "exited"
	}
	return nil
}

func (f *Fake) RemoveContainer(_ context.Context, id string, force, removeVolumes bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("RemoveContainer", id, fmt.Sprintf("force=%t", force), fmt.Sprintf("volumes=%t", removeVolumes))
	for i := range f.Containers {
		if f.Containers[i].ID == id || f.Containers[i].Name == id {
			f.Containers = append(f.Containers[:i], f.Containers[i+1:]...)
			break
		}
	}
	return nil
}

func (f *Fake) ComposeVersion(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ComposeVersion")
	return "2.40.0", nil
}

func (f *Fake) CleanupDerivedImages(_ context.Context, workspaceID string) (*container.CleanupResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CleanupDerivedImages", workspaceID)
	return &container.CleanupResult{}, nil
}

func (f *Fake) CleanupDanglingImages(context.Context) (*container.CleanupResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CleanupDanglingImages")
	return &container.CleanupResult{}, nil
}

func (f *Fake) DerivedImageStats(context.Context) (int, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("DerivedImageStats")
	return 0, 0, nil
}

func (f *Fake) RemoveVolume(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("RemoveVolume", name)
	return nil
}

func (f *Fake) Info(context.Context) (*container.SystemInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Info")
	return &container.SystemInfo{NCPU: 8, MemTotal: 16 << 30, ServerVersion: "28.0.0", SELinux: f.SELinux}, nil
}

func (f *Fake) CreateContainer(_ context.Context, opts container.ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateContainer", opts.Name, opts.Image)
	f.Specs = append(f.Specs, opts)
	f.created++
	id := fmt.Sprintf("fake-%d", f.created)
	f.Containers = append(f.Containers, state.ContainerSummary{ID: id, Name: opts.Name, State: "created", Labels: opts.Labels})
	return id, nil
}

func (f *Fake) StartContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("StartContainer", id)
	if c := f.find(id); c != nil {
		c.Running, c.State = true, "running"
	}
	return nil
}

func (f *Fake) PortMapping(_ context.Context, name string, port int, proto string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("PortMapping", name, fmt.Sprint(port), proto)
	if p, ok := f.Ports[fmt.Sprintf("%s/%d", name, port)]; ok {
		return p, nil
	}
	return 0, fmt.Errorf("port %d not published for %s", port, name)
}

func (f *Fake) Logs(_ context.Context, id string, _ container.LogsOptions) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Logs", id)
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *Fake) ImageExists(_ context.Context, ref string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ImageExists", ref)
	return f.Images[ref], nil
}

func (f *Fake) ImageID(_ context.Context, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ImageID", ref)
	if !f.Images[ref] {
		return "", fmt.Errorf("image not found: %s", ref)
	}
	return "sha256:" + ref, nil
}

func (f *Fake) ImageLabels(_ context.Context, ref string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ImageLabels", ref)
	if labels, ok := f.LocalImageLabels[ref]; ok {
		return labels, nil
	}
	return nil, fmt.Errorf("image not found: %s", ref)
}

func (f *Fake) ImageConfig(_ context.Context, ref string) (build.ImageConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ImageConfig", ref)
	return f.ImageConfigs[ref], nil
}

func (f *Fake) PullImageWithProgress(_ context.Context, ref string, _ io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("PullImageWithProgress", ref)
	labels, ok := f.Registry[ref]
	if !ok {
		return fmt.Errorf("pull %s: not found", ref)
	}
	if f.Images == nil {
		f.Images = map[string]bool{}
	}
	if f.LocalImageLabels == nil {
		f.LocalImageLabels = map[string]map[string]string{}
	}
	f.Images[ref] = true
	f.LocalImageLabels[ref] = labels
	return nil
}

func (f *Fake) BuildImage(_ context.Context, opts build.DockerfileBuildOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("BuildImage", opts.Tag)
	f.Builds = append(f.Builds, opts)
	if f.Images == nil {
		f.Images = map[string]bool{}
	}
	f.Images[opts.Tag] = true
	return nil
}

func (f *Fake) CopyToContainer(_ context.Context, src, name, dest string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CopyToContainer", src, name, dest)
	if f.Files == nil {
		f.Files = map[string][]byte{}
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f.Files[name+":"+dest] = data
	return nil
}

func (f *Fake) Compose(_ context.Context, p container.ComposeProject, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Compose", append([]string{p.Name}, args...)...)
	return nil
}

func (f *Fake) ComposeOutput(_ context.Context, p container.ComposeProject, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ComposeOutput", append([]string{p.Name}, args...)...)
	if out, ok := f.ComposeOutputs[strings.Join(args, " ")]; ok {
		return []byte(out), nil
	}
	return nil, fmt.Errorf("compose failed")
}

func (f *Fake) ExecInContainer(_ context.Context, name string, argv []string) error {
	f.mu.Lock()
	f.record("ExecInContainer", append([]string{name}, argv...)...)
	fn := f.ExecFunc
	f.mu.Unlock()
	if fn != nil {
		return fn(name, argv)
	}
	return nil
}

func (f *Fake) ExecOutput(_ context.Context, name string, argv []string) ([]byte, error) {
	f.mu.Lock()
	f.record("ExecOutput", append([]string{name}, argv...)...)
	out, ok := f.Outputs[strings.Join(argv, " ")]
	fn := f.OutputFunc
	f.mu.Unlock()
	if ok {
		return []byte(out), nil
	}
	if fn != nil {
		return fn(name, argv)
	}
	return nil, fmt.Errorf("exec failed")
}

// FileMarkers makes marker queries (`sh -c … <path>` printing "done") and
// `cat <path>` answer from Files, so lifecycle markers written with
// WriteFileInContainer are found (and read) again.
func (f *Fake) FileMarkers() {
	f.OutputFunc = func(name string, argv []string) ([]byte, error) {
		read := func(path string) ([]byte, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			data, ok := f.Files[name+":"+path]
			return data, ok
		}
		switch {
		case len(argv) == 5 && argv[0] == "sh" && argv[3] == "dcx-marker":
			if _, ok := read(argv[4]); ok {
				return []byte("done\n"), nil
			}
			return nil, nil
		case len(argv) == 2 && argv[0] == "cat":
			if data, ok := read(argv[1]); ok {
				return data, nil
			}
			return nil, fmt.Errorf("cat: %s: no such file", argv[1])
		}
		return nil, fmt.Errorf("exec failed")
	}
}

func (f *Fake) ExecOutputAs(_ context.Context, name, user string, argv []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ExecOutputAs", append([]string{name, user}, argv...)...)
	if out, ok := f.Outputs[strings.Join(argv, " ")]; ok {
		return []byte(out), nil
	}
	return nil, fmt.Errorf("exec failed")
}

func (f *Fake) ExecAsRoot(_ context.Context, name string, argv []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ExecAsRoot", append([]string{name}, argv...)...)
	if out, ok := f.Outputs[strings.Join(argv, " ")]; ok {
		return []byte(out), nil
	}
	if f.ExecFunc != nil {
		return nil, f.ExecFunc(name, argv)
	}
	return nil, nil
}

func (f *Fake) ExecDetachedAsRoot(_ context.Context, name string, argv []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ExecDetachedAsRoot", append([]string{name}, argv...)...)
	return nil
}

func (f *Fake) MkdirInContainer(_ context.Context, name, path, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("MkdirInContainer", name, path, user)
	return nil
}

func (f *Fake) ChmodInContainer(_ context.Context, name, path, mode, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ChmodInContainer", name, path, mode, user)
	return nil
}

func (f *Fake) ChownInContainer(_ context.Context, name, path, owner string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ChownInContainer", name, path, owner)
	return nil
}

func (f *Fake) WriteFileInContainer(_ context.Context, name, path string, content []byte, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("WriteFileInContainer", name, path, user)
	if f.Files == nil {
		f.Files = map[string][]byte{}
	}
	f.Files[name+":"+path] = append([]byte(nil), content...)
	return nil
}

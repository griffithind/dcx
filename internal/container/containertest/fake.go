// Package containertest provides an in-memory container.Engine for tests.
package containertest

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

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
	// ImageLabels maps image refs to their labels.
	ImageLabels map[string]map[string]string
	// ExecFunc decides the result of ExecInContainer (nil = success).
	ExecFunc func(containerName string, argv []string) error
	// Files records WriteFileInContainer content by "<container>:<path>".
	Files map[string][]byte

	calls []Call
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
	return &state.ContainerDetails{ID: c.ID, Name: c.Name, State: c.State, Running: c.Running, Labels: c.Labels}, nil
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

func (f *Fake) Info(context.Context) (*container.SystemInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("Info")
	return &container.SystemInfo{NCPU: 8, MemTotal: 16 << 30}, nil
}

func (f *Fake) CreateContainer(_ context.Context, opts container.CreateContainerOptions) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("CreateContainer", opts.Name, opts.Image)
	id := fmt.Sprintf("fake-%d", len(f.Containers)+1)
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

func (f *Fake) GetLogs(_ context.Context, id string, _ container.LogsOptions) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetLogs", id)
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *Fake) ImageExists(_ context.Context, ref string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ImageExists", ref)
	return f.Images[ref], nil
}

func (f *Fake) GetImageLabels(_ context.Context, ref string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("GetImageLabels", ref)
	if labels, ok := f.ImageLabels[ref]; ok {
		return labels, nil
	}
	return nil, fmt.Errorf("image not found: %s", ref)
}

func (f *Fake) PullImageWithProgress(_ context.Context, ref string, _ io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("PullImageWithProgress", ref)
	if f.Images == nil {
		f.Images = map[string]bool{}
	}
	f.Images[ref] = true
	return nil
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

func (f *Fake) ExecDetached(_ context.Context, name string, argv []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("ExecDetached", append([]string{name}, argv...)...)
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

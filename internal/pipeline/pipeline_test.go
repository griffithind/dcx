package pipeline

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/workspace"
)

func TestPlanAction_String(t *testing.T) {
	tests := []struct {
		action PlanAction
		expect string
	}{
		{ActionNone, "none"},
		{ActionCreate, "create"},
		{ActionRecreate, "recreate"},
		{ActionStart, "start"},
		{ActionRebuild, "rebuild"},
	}

	for _, tc := range tests {
		if string(tc.action) != tc.expect {
			t.Errorf("expected %q, got %q", tc.expect, string(tc.action))
		}
	}
}

func TestStage_String(t *testing.T) {
	tests := []struct {
		stage  Stage
		expect string
	}{
		{StageParse, "parse"},
		{StageResolve, "resolve"},
		{StagePlan, "plan"},
		{StageBuild, "build"},
		{StageDeploy, "deploy"},
	}

	for _, tc := range tests {
		if string(tc.stage) != tc.expect {
			t.Errorf("expected %q, got %q", tc.expect, string(tc.stage))
		}
	}
}

func TestNullProgressReporter(t *testing.T) {
	// Should not panic
	r := NullProgressReporter{}
	r.OnProgress(StageProgress{Stage: StageParse, Message: "test", Percentage: 50})
	r.OnStageStart(StageParse)
	r.OnStageComplete(StageParse, nil)
}

type mockProgressReporter struct {
	stages    []Stage
	completed []Stage
	messages  []string
}

func (m *mockProgressReporter) OnProgress(p StageProgress) {
	m.messages = append(m.messages, p.Message)
}

func (m *mockProgressReporter) OnStageStart(s Stage) {
	m.stages = append(m.stages, s)
}

func (m *mockProgressReporter) OnStageComplete(s Stage, err error) {
	m.completed = append(m.completed, s)
}

func TestExecutorDiscoverConfig(t *testing.T) {
	// Create a temporary test directory structure
	t.Run("config not found", func(t *testing.T) {
		e := NewExecutor(ExecutorOptions{})
		_, err := e.discoverConfig("/nonexistent/path", nil)
		if err == nil {
			t.Error("expected error for nonexistent config")
		}
	})
}

func TestContainerStatusFromDockerState(t *testing.T) {
	tests := []struct {
		state    string
		expected workspace.ContainerStatus
	}{
		{"running", workspace.StatusRunning},
		{"created", workspace.StatusCreated},
		{"exited", workspace.StatusStopped},
		{"dead", workspace.StatusStopped},
		{"unknown", workspace.StatusAbsent},
		{"", workspace.StatusAbsent},
	}

	for _, tc := range tests {
		t.Run(tc.state, func(t *testing.T) {
			result := containerStatusFromDockerState(tc.state)
			if result != tc.expected {
				t.Errorf("for state %q: expected %v, got %v", tc.state, tc.expected, result)
			}
		})
	}
}

func TestPlanImages(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})

	t.Run("image plan no features", func(t *testing.T) {
		ws := &workspace.Workspace{
			ID: "test12345678",
			Resolved: &workspace.ResolvedConfig{
				PlanType: workspace.PlanTypeImage,
				Image:    "ubuntu:latest",
			},
		}
		plans := e.planImages(ws)
		if len(plans) != 0 {
			t.Errorf("expected 0 plans for image without features, got %d", len(plans))
		}
	})

	t.Run("image plan with features", func(t *testing.T) {
		ws := &workspace.Workspace{
			ID: "test12345678",
			Resolved: &workspace.ResolvedConfig{
				PlanType: workspace.PlanTypeImage,
				Image:    "ubuntu:latest",
				Features: []*workspace.ResolvedFeature{
					{ID: "ghcr.io/devcontainers/features/go:1"},
				},
			},
		}
		plans := e.planImages(ws)
		if len(plans) != 1 {
			t.Fatalf("expected 1 plan for image with features, got %d", len(plans))
		}
		if plans[0].BaseImage != "ubuntu:latest" {
			t.Errorf("expected base image %q, got %q", "ubuntu:latest", plans[0].BaseImage)
		}
		if len(plans[0].Features) != 1 {
			t.Errorf("expected 1 feature, got %d", len(plans[0].Features))
		}
	})

	t.Run("dockerfile plan", func(t *testing.T) {
		ws := &workspace.Workspace{
			ID: "test12345678",
			Resolved: &workspace.ResolvedConfig{
				PlanType: workspace.PlanTypeDockerfile,
				Dockerfile: &workspace.DockerfilePlan{
					Path:    "/path/to/Dockerfile",
					Context: "/path/to",
				},
			},
		}
		plans := e.planImages(ws)
		if len(plans) != 1 {
			t.Fatalf("expected 1 plan for dockerfile, got %d", len(plans))
		}
		if plans[0].Dockerfile != "/path/to/Dockerfile" {
			t.Errorf("expected dockerfile %q, got %q", "/path/to/Dockerfile", plans[0].Dockerfile)
		}
	})

	t.Run("dockerfile plan with features", func(t *testing.T) {
		ws := &workspace.Workspace{
			ID: "test12345678",
			Resolved: &workspace.ResolvedConfig{
				PlanType: workspace.PlanTypeDockerfile,
				Dockerfile: &workspace.DockerfilePlan{
					Path:    "/path/to/Dockerfile",
					Context: "/path/to",
				},
				Features: []*workspace.ResolvedFeature{
					{ID: "feature1"},
					{ID: "feature2"},
				},
			},
		}
		plans := e.planImages(ws)
		if len(plans) != 2 {
			t.Fatalf("expected 2 plans for dockerfile with features, got %d", len(plans))
		}
		// First should be Dockerfile build
		if plans[0].Dockerfile == "" {
			t.Error("first plan should be Dockerfile build")
		}
		// Second should be feature derivation
		if len(plans[1].Features) != 2 {
			t.Errorf("second plan should have 2 features, got %d", len(plans[1].Features))
		}
	})
}

func TestPlanContainers(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})

	ws := &workspace.Workspace{
		ID: "test12345678",
		Resolved: &workspace.ResolvedConfig{
			ServiceName: "my-container",
			FinalImage:  "my-image:latest",
		},
	}

	plans := e.planContainers(ws)

	if len(plans) != 1 {
		t.Fatalf("expected 1 container plan, got %d", len(plans))
	}

	if plans[0].Name != "my-container" {
		t.Errorf("expected name %q, got %q", "my-container", plans[0].Name)
	}
	if !plans[0].IsPrimary {
		t.Error("primary container should be marked as primary")
	}
}

func TestPlanResult(t *testing.T) {
	result := &PlanResult{
		Action:  ActionRecreate,
		Reason:  "configuration changed",
		Changes: []string{"devcontainer.json changed", "Dockerfile changed"},
		ImagesToBuild: []ImageBuildPlan{
			{Tag: "test-image:latest", Reason: "rebuild"},
		},
		ContainersToCreate: []ContainerPlan{
			{Name: "test-container", IsPrimary: true},
		},
	}

	if result.Action != ActionRecreate {
		t.Errorf("expected action %v, got %v", ActionRecreate, result.Action)
	}
	if len(result.Changes) != 2 {
		t.Errorf("expected 2 changes, got %d", len(result.Changes))
	}
	if len(result.ImagesToBuild) != 1 {
		t.Errorf("expected 1 image to build, got %d", len(result.ImagesToBuild))
	}
	if len(result.ContainersToCreate) != 1 {
		t.Errorf("expected 1 container to create, got %d", len(result.ContainersToCreate))
	}
}

func TestBuildResult(t *testing.T) {
	result := &BuildResult{
		ImagesBuilt: []BuiltImage{
			{Tag: "base:latest", BuildTime: 1000},
			{Tag: "derived:latest", BuildTime: 500},
		},
		DerivedImage:    "derived:latest",
		BaseImagePulled: true,
	}

	if len(result.ImagesBuilt) != 2 {
		t.Errorf("expected 2 images built, got %d", len(result.ImagesBuilt))
	}
	if result.DerivedImage != "derived:latest" {
		t.Errorf("expected derived image %q, got %q", "derived:latest", result.DerivedImage)
	}
	if !result.BaseImagePulled {
		t.Error("base image should be marked as pulled")
	}
}

func TestDeployResult(t *testing.T) {
	result := &DeployResult{
		ContainerID:   "abc123",
		ContainerName: "my-container",
		AllContainers: []DeployedContainer{
			{ID: "abc123", Name: "my-container", IsPrimary: true, Status: "running"},
			{ID: "def456", Name: "db", IsPrimary: false, Status: "running"},
		},
		LifecycleHooksRun: []string{"onCreateCommand", "postCreateCommand"},
	}

	if result.ContainerID != "abc123" {
		t.Errorf("expected container ID %q, got %q", "abc123", result.ContainerID)
	}
	if len(result.AllContainers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(result.AllContainers))
	}
	if len(result.LifecycleHooksRun) != 2 {
		t.Errorf("expected 2 hooks run, got %d", len(result.LifecycleHooksRun))
	}
}

// Integration test for executor creation
func TestNewExecutor(t *testing.T) {
	t.Run("with defaults", func(t *testing.T) {
		e := NewExecutor(ExecutorOptions{})
		if e.progress == nil {
			t.Error("progress should default to NullProgressReporter")
		}
		if e.logger == nil {
			t.Error("logger should default to slog.Default")
		}
	})

	t.Run("with custom progress", func(t *testing.T) {
		progress := &mockProgressReporter{}
		e := NewExecutor(ExecutorOptions{Progress: progress})
		if e.progress != progress {
			t.Error("custom progress reporter should be used")
		}
	})
}

// Test that parse handles missing config properly
func TestExecutorParse_NoConfig(t *testing.T) {
	e := NewExecutor(ExecutorOptions{})

	_, err := e.Parse(context.Background(), ParseOptions{
		WorkspaceRoot: "/nonexistent/path",
	})

	if err == nil {
		t.Error("expected error when config not found")
	}
}

package compose

import (
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/stretchr/testify/assert"
)

func TestLoadOptions(t *testing.T) {
	tests := []struct {
		name string
		opts LoadOptions
	}{
		{
			name: "minimal options",
			opts: LoadOptions{
				Files: []string{"docker-compose.yml"},
			},
		},
		{
			name: "with project name",
			opts: LoadOptions{
				Files:       []string{"docker-compose.yml"},
				ProjectName: "myproject",
			},
		},
		{
			name: "with profiles",
			opts: LoadOptions{
				Files:    []string{"docker-compose.yml"},
				Profiles: []string{"dev", "debug"},
			},
		},
		{
			name: "with env files",
			opts: LoadOptions{
				Files:    []string{"docker-compose.yml"},
				EnvFiles: []string{".env", ".env.local"},
			},
		},
		{
			name: "with environment",
			opts: LoadOptions{
				Files:       []string{"docker-compose.yml"},
				Environment: map[string]string{"FOO": "bar"},
			},
		},
		{
			name: "with interpolation",
			opts: LoadOptions{
				Files:       []string{"docker-compose.yml"},
				Interpolate: true,
			},
		},
		{
			name: "with resolved paths",
			opts: LoadOptions{
				Files:        []string{"docker-compose.yml"},
				ResolvePaths: true,
			},
		},
		{
			name: "full options",
			opts: LoadOptions{
				Files:        []string{"docker-compose.yml", "docker-compose.override.yml"},
				WorkDir:      "/project",
				ProjectName:  "myproject",
				Profiles:     []string{"dev"},
				EnvFiles:     []string{".env"},
				Environment:  map[string]string{"DEBUG": "true"},
				Interpolate:  true,
				ResolvePaths: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotNil(t, tt.opts.Files)
		})
	}
}

func TestGetServiceNames(t *testing.T) {
	tests := []struct {
		name    string
		project *types.Project
		want    []string
		wantLen int
	}{
		{
			name:    "nil project",
			project: nil,
			want:    nil,
			wantLen: 0,
		},
		{
			name: "empty services",
			project: &types.Project{
				Services: types.Services{},
			},
			wantLen: 0,
		},
		{
			name: "single service",
			project: &types.Project{
				Services: types.Services{
					"app": types.ServiceConfig{Name: "app"},
				},
			},
			wantLen: 1,
		},
		{
			name: "multiple services",
			project: &types.Project{
				Services: types.Services{
					"app":      types.ServiceConfig{Name: "app"},
					"db":       types.ServiceConfig{Name: "db"},
					"frontend": types.ServiceConfig{Name: "frontend"},
				},
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := GetServiceNames(tt.project)
			assert.Len(t, names, tt.wantLen)
		})
	}
}

func TestGetService(t *testing.T) {
	project := &types.Project{
		Services: types.Services{
			"app": types.ServiceConfig{
				Name:  "app",
				Image: "alpine:latest",
			},
			"db": types.ServiceConfig{
				Name:  "db",
				Image: "postgres:15",
			},
		},
	}

	tests := []struct {
		name        string
		project     *types.Project
		serviceName string
		wantFound   bool
		wantImage   string
	}{
		{
			name:        "nil project",
			project:     nil,
			serviceName: "app",
			wantFound:   false,
		},
		{
			name:        "existing service",
			project:     project,
			serviceName: "app",
			wantFound:   true,
			wantImage:   "alpine:latest",
		},
		{
			name:        "another existing service",
			project:     project,
			serviceName: "db",
			wantFound:   true,
			wantImage:   "postgres:15",
		},
		{
			name:        "non-existent service",
			project:     project,
			serviceName: "nonexistent",
			wantFound:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, found := GetService(tt.project, tt.serviceName)
			assert.Equal(t, tt.wantFound, found)
			if found {
				assert.Equal(t, tt.wantImage, svc.Image)
			}
		})
	}
}

func TestGetServiceImage(t *testing.T) {
	tests := []struct {
		name        string
		project     *types.Project
		serviceName string
		wantImage   string
	}{
		{
			name:        "nil project",
			project:     nil,
			serviceName: "app",
			wantImage:   "",
		},
		{
			name: "service with image",
			project: &types.Project{
				Services: types.Services{
					"app": types.ServiceConfig{
						Name:  "app",
						Image: "alpine:latest",
					},
				},
			},
			serviceName: "app",
			wantImage:   "alpine:latest",
		},
		{
			name: "service with build",
			project: &types.Project{
				Name: "myproject",
				Services: types.Services{
					"app": types.ServiceConfig{
						Name:  "app",
						Build: &types.BuildConfig{Context: "."},
					},
				},
			},
			serviceName: "app",
			wantImage:   "myproject-app",
		},
		{
			name: "non-existent service",
			project: &types.Project{
				Services: types.Services{},
			},
			serviceName: "nonexistent",
			wantImage:   "",
		},
		{
			name: "service without image or build",
			project: &types.Project{
				Services: types.Services{
					"app": types.ServiceConfig{
						Name: "app",
					},
				},
			},
			serviceName: "app",
			wantImage:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			image := GetServiceImage(tt.project, tt.serviceName)
			assert.Equal(t, tt.wantImage, image)
		})
	}
}

func TestHasBuildConfig(t *testing.T) {
	tests := []struct {
		name        string
		project     *types.Project
		serviceName string
		want        bool
	}{
		{
			name:        "nil project",
			project:     nil,
			serviceName: "app",
			want:        false,
		},
		{
			name: "service with build",
			project: &types.Project{
				Services: types.Services{
					"app": types.ServiceConfig{
						Name:  "app",
						Build: &types.BuildConfig{Context: "."},
					},
				},
			},
			serviceName: "app",
			want:        true,
		},
		{
			name: "service without build",
			project: &types.Project{
				Services: types.Services{
					"app": types.ServiceConfig{
						Name:  "app",
						Image: "alpine:latest",
					},
				},
			},
			serviceName: "app",
			want:        false,
		},
		{
			name: "non-existent service",
			project: &types.Project{
				Services: types.Services{},
			},
			serviceName: "nonexistent",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasBuildConfig(tt.project, tt.serviceName)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetDependencies(t *testing.T) {
	tests := []struct {
		name        string
		project     *types.Project
		serviceName string
		wantLen     int
	}{
		{
			name:        "nil project",
			project:     nil,
			serviceName: "app",
			wantLen:     0,
		},
		{
			name: "service with dependencies",
			project: &types.Project{
				Services: types.Services{
					"app": types.ServiceConfig{
						Name: "app",
						DependsOn: types.DependsOnConfig{
							"db":    types.ServiceDependency{Condition: "service_started"},
							"redis": types.ServiceDependency{Condition: "service_healthy"},
						},
					},
				},
			},
			serviceName: "app",
			wantLen:     2,
		},
		{
			name: "service without dependencies",
			project: &types.Project{
				Services: types.Services{
					"app": types.ServiceConfig{
						Name: "app",
					},
				},
			},
			serviceName: "app",
			wantLen:     0,
		},
		{
			name: "non-existent service",
			project: &types.Project{
				Services: types.Services{},
			},
			serviceName: "nonexistent",
			wantLen:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := GetDependencies(tt.project, tt.serviceName)
			assert.Len(t, deps, tt.wantLen)
		})
	}
}

func TestProjectWithOverrides(t *testing.T) {
	// Helper to create a fresh project for each test
	newProject := func() *types.Project {
		return &types.Project{
			Services: types.Services{
				"app": types.ServiceConfig{
					Name:  "app",
					Image: "alpine:latest",
				},
			},
		}
	}

	t.Run("nil project", func(t *testing.T) {
		result := ProjectWithOverrides(nil, map[string]ServiceOverride{})
		assert.Nil(t, result)
	})

	t.Run("empty overrides", func(t *testing.T) {
		project := newProject()
		result := ProjectWithOverrides(project, map[string]ServiceOverride{})
		svc, ok := result.Services["app"]
		assert.True(t, ok)
		assert.Equal(t, "alpine:latest", svc.Image)
	})

	t.Run("image override", func(t *testing.T) {
		project := newProject()
		overrides := map[string]ServiceOverride{
			"app": {Image: "custom:latest"},
		}
		result := ProjectWithOverrides(project, overrides)
		svc, ok := result.Services["app"]
		assert.True(t, ok)
		assert.Equal(t, "custom:latest", svc.Image)
	})

	t.Run("override non-existent service", func(t *testing.T) {
		project := newProject()
		overrides := map[string]ServiceOverride{
			"nonexistent": {Image: "custom:latest"},
		}
		result := ProjectWithOverrides(project, overrides)
		svc, ok := result.Services["app"]
		assert.True(t, ok)
		assert.Equal(t, "alpine:latest", svc.Image) // original unchanged
	})
}

func TestProjectWithOverridesLabels(t *testing.T) {
	project := &types.Project{
		Services: types.Services{
			"app": types.ServiceConfig{
				Name: "app",
			},
		},
	}

	overrides := map[string]ServiceOverride{
		"app": {
			Labels: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
	}

	result := ProjectWithOverrides(project, overrides)
	svc := result.Services["app"]
	assert.Equal(t, "value1", svc.Labels["key1"])
	assert.Equal(t, "value2", svc.Labels["key2"])
}

func TestProjectWithOverridesEnvironment(t *testing.T) {
	project := &types.Project{
		Services: types.Services{
			"app": types.ServiceConfig{
				Name: "app",
			},
		},
	}

	overrides := map[string]ServiceOverride{
		"app": {
			Environment: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
	}

	result := ProjectWithOverrides(project, overrides)
	svc := result.Services["app"]
	assert.NotNil(t, svc.Environment)
}

func TestProjectWithOverridesPorts(t *testing.T) {
	project := &types.Project{
		Services: types.Services{
			"app": types.ServiceConfig{
				Name: "app",
			},
		},
	}

	overrides := map[string]ServiceOverride{
		"app": {
			Ports: []PortOverride{
				{Container: 80, Host: 8080, Protocol: "tcp"},
			},
		},
	}

	result := ProjectWithOverrides(project, overrides)
	svc := result.Services["app"]
	assert.Len(t, svc.Ports, 1)
	assert.Equal(t, uint32(80), svc.Ports[0].Target)
}

func TestProjectWithOverridesVolumes(t *testing.T) {
	project := &types.Project{
		Services: types.Services{
			"app": types.ServiceConfig{
				Name: "app",
			},
		},
	}

	overrides := map[string]ServiceOverride{
		"app": {
			Volumes: []VolumeOverride{
				{Type: "bind", Source: "/host", Target: "/container"},
			},
		},
	}

	result := ProjectWithOverrides(project, overrides)
	svc := result.Services["app"]
	assert.Len(t, svc.Volumes, 1)
	assert.Equal(t, "/host", svc.Volumes[0].Source)
	assert.Equal(t, "/container", svc.Volumes[0].Target)
}

func TestServiceOverride(t *testing.T) {
	override := ServiceOverride{
		Image:       "custom:latest",
		Labels:      map[string]string{"key": "value"},
		Environment: map[string]string{"FOO": "bar"},
		Ports: []PortOverride{
			{Container: 80, Host: 8080, Protocol: "tcp"},
		},
		Volumes: []VolumeOverride{
			{Type: "bind", Source: "/host", Target: "/container", ReadOnly: true},
		},
	}

	assert.Equal(t, "custom:latest", override.Image)
	assert.Equal(t, "value", override.Labels["key"])
	assert.Equal(t, "bar", override.Environment["FOO"])
	assert.Len(t, override.Ports, 1)
	assert.Len(t, override.Volumes, 1)
}

func TestPortOverride(t *testing.T) {
	tests := []struct {
		name     string
		port     PortOverride
		wantHost int
		wantCont int
		wantProt string
	}{
		{
			name:     "tcp port",
			port:     PortOverride{Container: 80, Host: 8080, Protocol: "tcp"},
			wantHost: 8080,
			wantCont: 80,
			wantProt: "tcp",
		},
		{
			name:     "udp port",
			port:     PortOverride{Container: 53, Host: 5353, Protocol: "udp"},
			wantHost: 5353,
			wantCont: 53,
			wantProt: "udp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantHost, tt.port.Host)
			assert.Equal(t, tt.wantCont, tt.port.Container)
			assert.Equal(t, tt.wantProt, tt.port.Protocol)
		})
	}
}

func TestVolumeOverride(t *testing.T) {
	tests := []struct {
		name       string
		vol        VolumeOverride
		wantType   string
		wantRO     bool
	}{
		{
			name:     "bind mount",
			vol:      VolumeOverride{Type: "bind", Source: "/host", Target: "/container"},
			wantType: "bind",
			wantRO:   false,
		},
		{
			name:     "readonly bind mount",
			vol:      VolumeOverride{Type: "bind", Source: "/host", Target: "/container", ReadOnly: true},
			wantType: "bind",
			wantRO:   true,
		},
		{
			name:     "volume mount",
			vol:      VolumeOverride{Type: "volume", Source: "myvolume", Target: "/data"},
			wantType: "volume",
			wantRO:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantType, tt.vol.Type)
			assert.Equal(t, tt.wantRO, tt.vol.ReadOnly)
		})
	}
}

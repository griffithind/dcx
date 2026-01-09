package devcontainer

// ExecutionPlan defines what needs to be built/run for a devcontainer.
// This is a sealed interface that enables type-safe plan handling through
// Go's type switch mechanism.
//
// Usage:
//
//	switch p := plan.(type) {
//	case *ImagePlan:
//	    // Handle image pull
//	case *DockerfilePlan:
//	    // Handle dockerfile build
//	case *ComposePlan:
//	    // Handle compose up
//	}
type ExecutionPlan interface {
	// Type returns the plan type identifier.
	Type() PlanType

	// sealed prevents external implementations.
	sealed()
}

// ImagePlan represents a pre-built image configuration.
// Use this when the devcontainer specifies only an image reference.
type ImagePlan struct {
	// Image is the Docker image reference (e.g., "mcr.microsoft.com/devcontainers/go:1")
	Image string
}

// Type returns PlanTypeImage.
func (p *ImagePlan) Type() PlanType { return PlanTypeImage }

// sealed prevents external implementations.
func (p *ImagePlan) sealed() {}

// DockerfilePlan represents a Dockerfile-based build configuration.
// Use this when the devcontainer has a build section.
type DockerfilePlan struct {
	// Dockerfile is the absolute path to the Dockerfile.
	Dockerfile string

	// Context is the absolute path to the build context directory.
	Context string

	// Args are build arguments passed to docker build.
	Args map[string]string

	// Target is the target build stage (optional).
	Target string

	// CacheFrom is a list of images to use as cache sources.
	CacheFrom []string

	// Options are additional build options from devcontainer.json.
	Options []string

	// BaseImage is the base image extracted from the Dockerfile's FROM instruction.
	// This is populated during build resolution.
	BaseImage string
}

// Type returns PlanTypeDockerfile.
func (p *DockerfilePlan) Type() PlanType { return PlanTypeDockerfile }

// sealed prevents external implementations.
func (p *DockerfilePlan) sealed() {}

// ComposePlan represents a Docker Compose configuration.
// Use this when the devcontainer specifies dockerComposeFile.
type ComposePlan struct {
	// Files are the absolute paths to compose files.
	Files []string

	// Service is the primary service name to attach to.
	Service string

	// RunServices are additional services to start alongside the primary service.
	RunServices []string

	// ProjectName is the compose project name (sanitized for Docker).
	ProjectName string

	// WorkDir is the working directory for compose commands.
	WorkDir string
}

// Type returns PlanTypeCompose.
func (p *ComposePlan) Type() PlanType { return PlanTypeCompose }

// sealed prevents external implementations.
func (p *ComposePlan) sealed() {}

// NewImagePlan creates a new ImagePlan.
func NewImagePlan(image string) *ImagePlan {
	return &ImagePlan{Image: image}
}

// NewDockerfilePlan creates a new DockerfilePlan.
func NewDockerfilePlan(dockerfile, context string) *DockerfilePlan {
	return &DockerfilePlan{
		Dockerfile: dockerfile,
		Context:    context,
		Args:       make(map[string]string),
	}
}

// NewComposePlan creates a new ComposePlan.
func NewComposePlan(files []string, service, projectName string) *ComposePlan {
	return &ComposePlan{
		Files:       files,
		Service:     service,
		ProjectName: projectName,
	}
}

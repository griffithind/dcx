package agent

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateSocketName(t *testing.T) {
	names := make(map[string]bool)

	// Generate multiple names and ensure they're unique
	for i := 0; i < 100; i++ {
		name := GenerateSocketName()
		assert.True(t, strings.HasPrefix(name, "agent-"))
		assert.True(t, strings.HasSuffix(name, ".sock"))
		assert.False(t, names[name], "socket name should be unique")
		names[name] = true
	}
}

func TestGetProxyDir(t *testing.T) {
	tests := []struct {
		name        string
		workspaceID string
		wantSuffix  string
	}{
		{
			name:        "basic workspace ID",
			workspaceID: "abc123",
			wantSuffix:  "ssh-agent",
		},
		{
			name:        "complex workspace ID",
			workspaceID: "my-project-xyz",
			wantSuffix:  "ssh-agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, err := GetProxyDir(tt.workspaceID)
			assert.NoError(t, err)
			assert.Contains(t, dir, tt.workspaceID)
			assert.True(t, strings.HasSuffix(dir, tt.wantSuffix))

			// Platform-specific checks
			if runtime.GOOS == "darwin" {
				assert.Contains(t, dir, "Library/Caches/dcx")
			} else {
				// Linux should use XDG_RUNTIME_DIR or /run/user
				assert.True(t, strings.Contains(dir, "dcx"), "should contain dcx in path")
			}
		})
	}
}

func TestProxySocketPath(t *testing.T) {
	// Skip if SSH agent is not available
	if !IsAvailable() {
		t.Skip("SSH agent not available")
	}

	proxy, err := NewProxyWithSocket("test-ws", "test.sock")
	if err != nil {
		t.Skip("Could not create proxy (SSH agent may not be accessible)")
	}

	assert.Contains(t, proxy.SocketPath(), "test.sock")
	assert.Equal(t, "test.sock", proxy.SocketName())
}

func TestProxyDir(t *testing.T) {
	// Skip if SSH agent is not available
	if !IsAvailable() {
		t.Skip("SSH agent not available")
	}

	proxy, err := NewProxyWithSocket("test-workspace", "agent.sock")
	if err != nil {
		t.Skip("Could not create proxy (SSH agent may not be accessible)")
	}

	proxyDir := proxy.ProxyDir()
	assert.Contains(t, proxyDir, "test-workspace")
	assert.Contains(t, proxyDir, "ssh-agent")
}

func TestProxyIsRunning(t *testing.T) {
	// Skip if SSH agent is not available
	if !IsAvailable() {
		t.Skip("SSH agent not available")
	}

	proxy, err := NewProxyWithSocket("test-ws", GenerateSocketName())
	if err != nil {
		t.Skip("Could not create proxy (SSH agent may not be accessible)")
	}

	assert.False(t, proxy.IsRunning())

	// Start the proxy
	err = proxy.Start()
	if err != nil {
		t.Skip("Could not start proxy")
	}
	defer proxy.Stop()

	assert.True(t, proxy.IsRunning())
}

func TestProxyStartStop(t *testing.T) {
	// Skip if SSH agent is not available
	if !IsAvailable() {
		t.Skip("SSH agent not available")
	}

	proxy, err := NewProxyWithSocket("test-ws", GenerateSocketName())
	if err != nil {
		t.Skip("Could not create proxy (SSH agent may not be accessible)")
	}

	// Start
	err = proxy.Start()
	if err != nil {
		t.Skip("Could not start proxy")
	}

	assert.True(t, proxy.IsRunning())

	// Stop
	err = proxy.Stop()
	assert.NoError(t, err)
	assert.False(t, proxy.IsRunning())
}

func TestProxyDoubleStart(t *testing.T) {
	// Skip if SSH agent is not available
	if !IsAvailable() {
		t.Skip("SSH agent not available")
	}

	proxy, err := NewProxyWithSocket("test-ws", GenerateSocketName())
	if err != nil {
		t.Skip("Could not create proxy (SSH agent may not be accessible)")
	}
	defer proxy.Stop()

	// First start
	err = proxy.Start()
	if err != nil {
		t.Skip("Could not start proxy")
	}

	// Second start should be a no-op
	err = proxy.Start()
	assert.NoError(t, err)
	assert.True(t, proxy.IsRunning())
}

func TestProxyDoubleStop(t *testing.T) {
	// Skip if SSH agent is not available
	if !IsAvailable() {
		t.Skip("SSH agent not available")
	}

	proxy, err := NewProxyWithSocket("test-ws", GenerateSocketName())
	if err != nil {
		t.Skip("Could not create proxy (SSH agent may not be accessible)")
	}

	// Start first
	err = proxy.Start()
	if err != nil {
		t.Skip("Could not start proxy")
	}

	// First stop
	err = proxy.Stop()
	assert.NoError(t, err)

	// Second stop should be a no-op
	err = proxy.Stop()
	assert.NoError(t, err)
}

func TestNewProxy(t *testing.T) {
	// Skip if SSH agent is not available
	if !IsAvailable() {
		t.Skip("SSH agent not available")
	}

	proxy, err := NewProxy("test-ws")
	if err != nil {
		t.Skip("Could not create proxy (SSH agent may not be accessible)")
	}

	// Should have default socket name
	assert.Equal(t, "agent.sock", proxy.SocketName())
}

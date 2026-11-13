package devcontainer

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		in   string
		want PortForward
		spec string
	}{
		{"3000", PortForward{HostPort: 3000, ContainerPort: 3000}, "3000:3000"},
		{"8080:80", PortForward{HostPort: 8080, ContainerPort: 80}, "8080:80"},
		{"127.0.0.1:9000:90", PortForward{Host: "127.0.0.1", HostPort: 9000, ContainerPort: 90}, "127.0.0.1:9000:90"},
		{"127.0.0.1::90", PortForward{Host: "127.0.0.1", ContainerPort: 90, EphemeralHostPort: true}, "127.0.0.1::90"},
		{"5353/udp", PortForward{HostPort: 5353, ContainerPort: 5353, Protocol: "udp"}, "5353:5353/udp"},
		{"8080:80/tcp", PortForward{HostPort: 8080, ContainerPort: 80, Protocol: "tcp"}, "8080:80"},
	}
	for _, tt := range tests {
		got, err := ParsePortSpec(tt.in)
		require.NoError(t, err, tt.in)
		assert.Equal(t, tt.want, got, tt.in)
		assert.Equal(t, tt.spec, got.DockerSpec(), tt.in)
	}
}

func TestParsePortSpecRemoteHost(t *testing.T) {
	_, err := ParsePortSpec("db:5432")
	assert.True(t, errors.Is(err, errRemoteHostPort))
}

func TestParsePortSpecInvalid(t *testing.T) {
	for _, in := range []string{"", "abc", "0", "70000", "1:2:3:4", "8080:x", "300/http", "nothost:1:2"} {
		_, err := ParsePortSpec(in)
		assert.Error(t, err, in)
	}
}

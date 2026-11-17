package devcontainer

import (
	"encoding/json"
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

// Port ranges in docker -p syntax publish one binding per port (C17), as
// appPort strings do when the official CLI passes them to docker.
func TestParsePortSpecsRanges(t *testing.T) {
	specs := func(spec string) []string {
		t.Helper()
		pfs, err := ParsePortSpecs(spec)
		require.NoError(t, err, spec)
		out := make([]string, len(pfs))
		for i, pf := range pfs {
			out[i] = pf.DockerSpec()
		}
		return out
	}
	assert.Equal(t, []string{"3000:3000", "3001:3001", "3002:3002"}, specs("3000-3002"))
	assert.Equal(t, []string{"8000:3000", "8001:3001"}, specs("8000-8001:3000-3001"))
	assert.Equal(t, []string{"127.0.0.1:8000:3000", "127.0.0.1:8001:3001"}, specs("127.0.0.1:8000-8001:3000-3001"))
	assert.Equal(t, []string{"127.0.0.1::3000", "127.0.0.1::3001"}, specs("127.0.0.1::3000-3001"))
	assert.Equal(t, []string{"3000:3000/udp", "3001:3001/udp"}, specs("3000-3001/udp"))
	assert.Equal(t, []string{"3000:3000", "3001:3001"}, specs("localhost:3000-3001"))
	assert.Equal(t, []string{"8080:80"}, specs("8080:80"), "a single port is ParsePortSpec's")

	for spec, want := range map[string]string{
		"8000-8005:80":        "differ in length",
		"3002-3000":           "invalid port",
		"1-2000":              "more than 1024 ports",
		"3000-x":              "invalid port",
		"8000-8001:3000-3005": "differ in length",
	} {
		_, err := ParsePortSpecs(spec)
		require.Error(t, err, spec)
		assert.Contains(t, err.Error(), want, spec)
	}
}

func TestAppPortRangeExpands(t *testing.T) {
	var ports PortList
	require.NoError(t, json.Unmarshal([]byte(`["7000-7002:9000-9002", 3000]`), &ports))
	got := NewBuilder(nil).parsePorts("appPort", ports, true)
	require.Len(t, got, 4)
	assert.Equal(t, "7000:9000", got[0].DockerSpec(), "a string is passed as written: every interface")
	assert.Equal(t, "7002:9002", got[2].DockerSpec())
	assert.Equal(t, "127.0.0.1:3000:3000", got[3].DockerSpec())
}

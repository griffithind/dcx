package parse

import (
	"testing"
)

func TestParsePortBinding(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *PortBinding
	}{
		{
			name:  "container port only",
			input: "8080",
			expected: &PortBinding{
				HostPort:      "8080",
				ContainerPort: "8080",
				Protocol:      "tcp",
			},
		},
		{
			name:  "host:container port",
			input: "8080:80",
			expected: &PortBinding{
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "ip:host:container port",
			input: "127.0.0.1:8080:80",
			expected: &PortBinding{
				HostIP:        "127.0.0.1",
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "tcp",
			},
		},
		{
			name:  "with udp protocol",
			input: "8080/udp",
			expected: &PortBinding{
				HostPort:      "8080",
				ContainerPort: "8080",
				Protocol:      "udp",
			},
		},
		{
			name:  "full spec with udp",
			input: "127.0.0.1:8080:80/udp",
			expected: &PortBinding{
				HostIP:        "127.0.0.1",
				HostPort:      "8080",
				ContainerPort: "80",
				Protocol:      "udp",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "invalid port",
			input:    "abc",
			expected: nil,
		},
		{
			name:     "too many colons",
			input:    "1:2:3:4",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParsePortBinding(tt.input)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("expected %+v, got nil", tt.expected)
				return
			}

			if result.HostIP != tt.expected.HostIP {
				t.Errorf("HostIP: expected %q, got %q", tt.expected.HostIP, result.HostIP)
			}
			if result.HostPort != tt.expected.HostPort {
				t.Errorf("HostPort: expected %q, got %q", tt.expected.HostPort, result.HostPort)
			}
			if result.ContainerPort != tt.expected.ContainerPort {
				t.Errorf("ContainerPort: expected %q, got %q", tt.expected.ContainerPort, result.ContainerPort)
			}
			if result.Protocol != tt.expected.Protocol {
				t.Errorf("Protocol: expected %q, got %q", tt.expected.Protocol, result.Protocol)
			}
		})
	}
}

func TestParsePortBindings(t *testing.T) {
	specs := []string{"8080", "invalid", "9090:90", "8080/udp"}
	result := ParsePortBindings(specs)

	// Should skip invalid and return 3 valid bindings
	if len(result) != 3 {
		t.Errorf("expected 3 bindings, got %d", len(result))
	}
}

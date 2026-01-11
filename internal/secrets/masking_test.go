package secrets

import (
	"bytes"
	"testing"
)

func TestMaskingWriter(t *testing.T) {
	tests := []struct {
		name     string
		secrets  []Secret
		input    string
		expected string
	}{
		{
			name:     "no secrets",
			secrets:  nil,
			input:    "hello world",
			expected: "hello world",
		},
		{
			name: "single secret",
			secrets: []Secret{
				{Name: "S1", Value: []byte("secret")},
			},
			input:    "my secret value",
			expected: "my ******** value",
		},
		{
			name: "multiple secrets",
			secrets: []Secret{
				{Name: "S1", Value: []byte("password")},
				{Name: "S2", Value: []byte("token")},
			},
			input:    "password and token here",
			expected: "******** and ******** here",
		},
		{
			name: "overlapping secrets - longer first",
			secrets: []Secret{
				{Name: "S1", Value: []byte("secret")},
				{Name: "S2", Value: []byte("secret_longer")},
			},
			input:    "my secret_longer value",
			expected: "my ******** value",
		},
		{
			name: "secret not in input",
			secrets: []Secret{
				{Name: "S1", Value: []byte("notpresent")},
			},
			input:    "hello world",
			expected: "hello world",
		},
		{
			name: "multiple occurrences",
			secrets: []Secret{
				{Name: "S1", Value: []byte("pass")},
			},
			input:    "pass one pass two pass three",
			expected: "******** one ******** two ******** three",
		},
		{
			name: "empty secret value ignored",
			secrets: []Secret{
				{Name: "S1", Value: []byte("")},
				{Name: "S2", Value: []byte("real")},
			},
			input:    "this is real secret",
			expected: "this is ******** secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewMaskingWriter(&buf, tt.secrets)

			n, err := w.Write([]byte(tt.input))
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}

			if n != len(tt.input) {
				t.Errorf("Write() returned %d, want %d", n, len(tt.input))
			}

			if buf.String() != tt.expected {
				t.Errorf("Write() output = %q, want %q", buf.String(), tt.expected)
			}
		})
	}
}

func TestMaskString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		secrets  []Secret
		expected string
	}{
		{
			name:     "no secrets",
			input:    "hello world",
			secrets:  nil,
			expected: "hello world",
		},
		{
			name:  "mask single secret",
			input: "my password is secret123",
			secrets: []Secret{
				{Name: "P", Value: []byte("secret123")},
			},
			expected: "my password is ********",
		},
		{
			name:  "mask multiple secrets",
			input: "user: admin, pass: hunter2",
			secrets: []Secret{
				{Name: "U", Value: []byte("admin")},
				{Name: "P", Value: []byte("hunter2")},
			},
			expected: "user: ********, pass: ********",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MaskString(tt.input, tt.secrets)
			if result != tt.expected {
				t.Errorf("MaskString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

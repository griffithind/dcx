package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCommand_String(t *testing.T) {
	result := parseCommand("echo hello")
	assert.Equal(t, []string{"echo hello"}, result)
}

func TestParseCommand_StringArray(t *testing.T) {
	result := parseCommand([]string{"echo", "hello", "world"})
	assert.Equal(t, []string{"echo hello world"}, result)
}

func TestParseCommand_InterfaceArray(t *testing.T) {
	result := parseCommand([]interface{}{"echo", "hello"})
	assert.Equal(t, []string{"echo hello"}, result)
}

func TestParseCommand_Map(t *testing.T) {
	result := parseCommand(map[string]interface{}{
		"task1": "echo first",
		"task2": "echo second",
	})
	// Maps have no guaranteed order, so just check length
	assert.Len(t, result, 2)
}

func TestParseCommand_Nil(t *testing.T) {
	result := parseCommand(nil)
	assert.Nil(t, result)
}

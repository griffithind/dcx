package lifecyclecmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parse(t *testing.T, js string) *Command {
	t.Helper()
	var c Command
	require.NoError(t, json.Unmarshal([]byte(js), &c))
	return &c
}

func TestSteps(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		want     []Step
		parallel bool
	}{
		{"string uses the shell", `"echo hello && echo world"`, []Step{{Shell: "echo hello && echo world"}}, false},
		{"array keeps arguments with spaces", `["git", "clone", "repo with spaces.git"]`,
			[]Step{{Argv: []string{"git", "clone", "repo with spaces.git"}}}, false},
		{"non-string array items are ignored", `["echo", 1, "x"]`, []Step{{Argv: []string{"echo", "x"}}}, false},
		{"object is named, sorted and parallel", `{"c": "3", "a": "1", "b": ["2"]}`,
			[]Step{{Name: "a", Shell: "1"}, {Name: "b", Argv: []string{"2"}}, {Name: "c", Shell: "3"}}, true},
		{"blank string runs nothing", `"   "`, nil, false},
		{"empty array runs nothing", `[]`, nil, false},
		{"empty named values are skipped", `{"a": "", "b": "x"}`, []Step{{Name: "b", Shell: "x"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := parse(t, tt.json)
			assert.Equal(t, tt.want, c.Steps())
			assert.Equal(t, tt.parallel, c.Parallel())
		})
	}
	var absent *Command
	assert.Nil(t, absent.Steps())
}

// Commands marshal back to the form they were written in: the config hash
// (the staleness key) depends on it.
func TestMarshalRoundTrip(t *testing.T) {
	for _, js := range []string{`"npm install"`, `""`, `["npm","ci"]`, `[]`, `{"a":"echo a","b":["echo","b"]}`} {
		out, err := json.Marshal(parse(t, js))
		require.NoError(t, err)
		assert.JSONEq(t, js, string(out))
		assert.Equal(t, js, string(out))
	}
}

func TestRejectsOtherTypes(t *testing.T) {
	var c Command
	assert.Error(t, json.Unmarshal([]byte(`42`), &c))
}

func TestStepString(t *testing.T) {
	assert.Equal(t, "echo hello", Step{Argv: []string{"echo", "hello"}}.String())
	assert.Equal(t, "[install] npm install", Step{Name: "install", Argv: []string{"npm", "install"}}.String())
	assert.Equal(t, "echo a && echo b", Step{Shell: "echo a && echo b"}.String())
}

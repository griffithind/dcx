package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Labels containing commas (JSON values, paths) must survive intact; the old
// `docker ps` Labels-column parsing split them apart.
func TestParseContainerSummariesKeepsCommasInLabels(t *testing.T) {
	data := []byte(`[{
		"Id": "abc123",
		"Name": "/my-container",
		"State": {"Status": "running", "Running": true},
		"Config": {"Labels": {
			"com.griffithind.dcx.config.path": "/home/u/a,b/.devcontainer/devcontainer.json",
			"com.griffithind.dcx.features.config": "{\"node\":{\"version\":\"18\"},\"go\":{}}"
		}}
	}, {
		"Id": "def456",
		"Name": "/stopped",
		"State": {"Status": "exited", "Running": false},
		"Config": {"Labels": null}
	}]`)

	got, err := parseContainerSummaries(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d summaries, want 2", len(got))
	}
	if got[0].Name != "my-container" || !got[0].Running || got[0].State != "running" {
		t.Errorf("unexpected first summary: %+v", got[0])
	}
	if p := got[0].Labels["com.griffithind.dcx.config.path"]; p != "/home/u/a,b/.devcontainer/devcontainer.json" {
		t.Errorf("config.path label = %q", p)
	}
	if f := got[0].Labels["com.griffithind.dcx.features.config"]; f != `{"node":{"version":"18"},"go":{}}` {
		t.Errorf("features.config label = %q", f)
	}
	if got[1].Running || got[1].Labels == nil {
		t.Errorf("unexpected second summary: %+v", got[1])
	}
}

func TestIsDcxImage(t *testing.T) {
	cases := map[string]bool{
		"dcx/abc123def456":          true,
		"dcx-derived-abc123def456":  true,
		"dcx-derived-temp":          true,
		"alpine":                    false,
		"mcr.microsoft.com/dcx/foo": false,
		"mydcx/app":                 false,
	}
	for repo, want := range cases {
		if got := isDcxImage(imageInfo{Repository: repo}); got != want {
			t.Errorf("isDcxImage(%q) = %v, want %v", repo, got, want)
		}
	}
}

func TestParseImageSize(t *testing.T) {
	for in, want := range map[string]int64{
		"":         0,
		"512B":     512,
		"1.5KB":    1536,
		"2MB":      2 << 20,
		" 1.25GB ": 1342177280,
		"77":       77,
	} {
		assert.Equal(t, want, parseImageSize(in), "parseImageSize(%q)", in)
	}
}

func TestLastLineTakesIDAfterWarnings(t *testing.T) {
	assert.Equal(t, "abc123", lastLine("abc123\n"))
	assert.Equal(t, "abc123", lastLine("WARNING: something\nabc123\n\n"))
	assert.Equal(t, "", lastLine("  \n"))
}

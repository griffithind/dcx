package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

// ImageExists checks if an image exists locally.
func (c *Client) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	_, _, err := c.cli.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetImageLabels returns the labels for an image.
func (c *Client) GetImageLabels(ctx context.Context, imageRef string) (map[string]string, error) {
	info, _, err := c.cli.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}
	if info.Config == nil {
		return nil, nil
	}
	return info.Config.Labels, nil
}

// PullImage pulls an image from a registry.
func (c *Client) PullImage(ctx context.Context, imageRef string) error {
	return c.PullImageWithProgress(ctx, imageRef, nil)
}

// PullImageWithProgress pulls an image with optional progress display.
// If progressOut is nil, progress is discarded.
func (c *Client) PullImageWithProgress(ctx context.Context, imageRef string, progressOut io.Writer) error {
	reader, err := c.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	if progressOut == nil {
		// No progress display - just consume output
		_, err = io.Copy(io.Discard, reader)
		return err
	}

	// Process with progress display
	display := newProgressDisplay(progressOut)
	return display.processPullOutput(reader)
}

// progressDisplay handles Docker progress output.
type progressDisplay struct {
	out io.Writer
}

func newProgressDisplay(out io.Writer) *progressDisplay {
	return &progressDisplay{out: out}
}

// progressEvent represents a Docker progress event.
type progressEvent struct {
	ID             string         `json:"id,omitempty"`
	Status         string         `json:"status,omitempty"`
	Progress       string         `json:"progress,omitempty"`
	ProgressDetail progressDetail `json:"progressDetail,omitempty"`
	Stream         string         `json:"stream,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type progressDetail struct {
	Current int64 `json:"current,omitempty"`
	Total   int64 `json:"total,omitempty"`
}

func (d *progressDisplay) processPullOutput(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	layers := make(map[string]string)
	var lastStatus string

	for {
		var event progressEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		if event.Error != "" {
			return fmt.Errorf("%s", event.Error)
		}

		// Track layer status
		if event.ID != "" {
			layers[event.ID] = event.Status
		}

		// Show meaningful status updates
		status := event.Status
		if event.ID != "" && event.Progress != "" {
			id := event.ID
			if len(id) > 12 {
				id = id[:12]
			}
			status = fmt.Sprintf("%s: %s %s", id, event.Status, event.Progress)
		} else if event.ID != "" {
			id := event.ID
			if len(id) > 12 {
				id = id[:12]
			}
			status = fmt.Sprintf("%s: %s", id, event.Status)
		}

		if status != "" && status != lastStatus {
			fmt.Fprintf(d.out, "%s\n", status)
			lastStatus = status
		}
	}

	return nil
}

package penman

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// SetupEvent is emitted once at the start of a stream. It describes the SVG
// canvas that the path events should be drawn onto.
type SetupEvent struct {
	Type    string `json:"type"`
	ViewBox string `json:"viewBox"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
}

// PathEvent is an incremental SVG path fragment for the handwriting effect.
type PathEvent struct {
	Type       string  `json:"type"`
	Data       string  `json:"data"`
	Color      string  `json:"color"`
	Width      float64 `json:"width"`
	LineNumber int     `json:"lineNumber"`
}

// StreamHandlers receives the callbacks for a handwriting stream. All fields
// are optional.
type StreamHandlers struct {
	OnSetup func(SetupEvent)
	OnPath  func(PathEvent)
	OnError func(string)
	OnDone  func()
}

// StreamHandwriting connects to /handwriting/generate-stream and invokes the
// handlers for each server-sent event until the stream is complete, the
// context is cancelled or an error occurs.
func (c *Client) StreamHandwriting(config HandwritingConfig, handlers StreamHandlers, opts ...RequestOptions) error {
	o := firstOptions(opts)

	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	parent := o.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	req, err := c.newRequest(ctx, http.MethodPost, HandwritingPathPrefix+"/generate-stream", bytes.NewReader(data), "application/json", o)
	if err != nil {
		handlers.reportError(err.Error())
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	var timer *time.Timer
	if timeout > 0 {
		timer = time.AfterFunc(timeout, cancel)
	}

	resp, err := c.httpClient(o).Do(req)
	if timer != nil {
		timer.Stop()
	}
	if err != nil {
		handlers.reportError(err.Error())
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(resp.Body)
		httpErr := newHandwritingError(resp.StatusCode, body)
		handlers.reportError(httpErr.Error())
		return httpErr
	}

	if err := consumeEventStream(resp.Body, handlers); err != nil {
		handlers.reportError(err.Error())
		return err
	}
	return nil
}

// StreamHandwriting calls StreamHandwriting on the default client.
func StreamHandwriting(config HandwritingConfig, handlers StreamHandlers, opts ...RequestOptions) error {
	return defaultClient.StreamHandwriting(config, handlers, opts...)
}

func consumeEventStream(body io.Reader, handlers StreamHandlers) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}

		var event struct {
			Type       string  `json:"type"`
			ViewBox    string  `json:"viewBox"`
			Width      float64 `json:"width"`
			Height     float64 `json:"height"`
			Data       string  `json:"data"`
			Color      string  `json:"color"`
			LineNumber int     `json:"lineNumber"`
			Message    string  `json:"message"`
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			handlers.reportError("failed to parse server data")
			continue
		}

		switch event.Type {
		case "setup":
			if handlers.OnSetup != nil {
				handlers.OnSetup(SetupEvent{
					Type:    event.Type,
					ViewBox: event.ViewBox,
					Width:   int(event.Width),
					Height:  int(event.Height),
				})
			}
		case "path":
			if handlers.OnPath != nil {
				handlers.OnPath(PathEvent{
					Type:       event.Type,
					Data:       event.Data,
					Color:      event.Color,
					Width:      event.Width,
					LineNumber: event.LineNumber,
				})
			}
		case "error":
			handlers.reportError(event.Message)
		case "done":
			if handlers.OnDone != nil {
				handlers.OnDone()
			}
			return nil
		}
	}

	return scanner.Err()
}

func (h StreamHandlers) reportError(message string) {
	if h.OnError != nil {
		h.OnError(message)
	}
}

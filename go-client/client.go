// Package penman is a small Go client for the Penman handwriting API.
package penman

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the address the backend listens on during local
	// development (see docker-compose.yml and backend/run.sh).
	DefaultBaseURL = "http://localhost:8000"

	// HandwritingPathPrefix is the prefix mounted by backend/main.py.
	HandwritingPathPrefix = "/handwriting"

	// DefaultTimeout is the time allowed for a request to be established.
	DefaultTimeout = 30 * time.Second
)

// HandwritingConfig is the request body accepted by the streaming endpoint.
// It mirrors the client-side HandwritingConfig in frontend/src/lib/api.ts.
type HandwritingConfig struct {
	TextInput   string  `json:"text_input"`
	Style       int     `json:"style"`
	Bias        float64 `json:"bias"`
	StrokeWidth int     `json:"stroke_width"`
	StrokeColor string  `json:"stroke_color"`
}

// DetailedHandwritingRequest is the body for /handwriting/generate.
type DetailedHandwritingRequest struct {
	TextInput    []string  `json:"text_input"`
	Styles       []int     `json:"styles"`
	Biases       []float64 `json:"biases"`
	StrokeWidths []int     `json:"stroke_widths"`
	StrokeColors []string  `json:"stroke_colors"`
	AsPDF        bool      `json:"as_pdf,omitempty"`
}

// SimpleHandwritingRequest is the body for /handwriting/generate-simple.
type SimpleHandwritingRequest struct {
	TextInput   string  `json:"text_input"`
	Style       int     `json:"style"`
	Bias        float64 `json:"bias"`
	StrokeWidth int     `json:"stroke_width"`
	StrokeColor string  `json:"stroke_color"`
	AsPDF       bool    `json:"as_pdf,omitempty"`
}

// RequestOptions override the defaults for a single call.
type RequestOptions struct {
	// BaseURL overrides the Client base URL when non-empty.
	BaseURL string
	// Timeout bounds how long the request may take. A value of 0 uses
	// DefaultTimeout and a negative value disables the timeout.
	Timeout time.Duration
	// Headers are added to (or override) the Client headers.
	Headers map[string]string
	// Client overrides the underlying HTTP client.
	Client *http.Client
	// Context may be used to cancel a request, including a stream.
	Context context.Context
}

// Client talks to the Penman backend.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	Headers    map[string]string
}

// HandwritingError is returned when the backend responds with a non-2xx
// status code.
type HandwritingError struct {
	Message string
	Status  int
	Body    []byte
}

// Error implements the error interface.
func (e *HandwritingError) Error() string {
	if e == nil {
		return "penman: request failed"
	}
	if e.Status != 0 {
		return fmt.Sprintf("penman: %s (status %d)", e.Message, e.Status)
	}
	return "penman: " + e.Message
}

var defaultClient = NewClient(DefaultBaseURL)

// NewClient creates a Client pointed at baseURL. An empty baseURL uses
// DefaultBaseURL.
func NewClient(baseURL string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:    NormalizeBaseURL(baseURL),
		HTTPClient: &http.Client{},
		Headers:    make(map[string]string),
	}
}

// CreateClient is an alias for NewClient.
func CreateClient(baseURL string) *Client {
	return NewClient(baseURL)
}

// NormalizeBaseURL removes surrounding whitespace and any trailing slashes.
func NormalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// Generate renders one or more lines of handwriting and returns the raw
// response body, which is an SVG document or a PDF when AsPDF is set.
func (c *Client) Generate(request DetailedHandwritingRequest, opts ...RequestOptions) ([]byte, error) {
	return c.postJSON(HandwritingPathPrefix+"/generate", request, firstOptions(opts))
}

// GenerateSimple renders a single paragraph of handwriting.
func (c *Client) GenerateSimple(request SimpleHandwritingRequest, opts ...RequestOptions) ([]byte, error) {
	return c.postJSON(HandwritingPathPrefix+"/generate-simple", request, firstOptions(opts))
}

// SvgToPDF uploads an SVG document and returns the converted PDF bytes.
func (c *Client) SvgToPDF(file io.Reader, filename string, opts ...RequestOptions) ([]byte, error) {
	if filename == "" {
		filename = "input.svg"
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	o := firstOptions(opts)
	ctx, cancel := contextFor(o)
	defer cancel()

	req, err := c.newRequest(ctx, http.MethodPost, HandwritingPathPrefix+"/svg-to-pdf", &buf, writer.FormDataContentType(), o)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient(o).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, newHandwritingError(resp.StatusCode, body)
	}
	return body, nil
}

// ConvertSVGToPDF is an alias for SvgToPDF.
func (c *Client) ConvertSVGToPDF(file io.Reader, filename string, opts ...RequestOptions) ([]byte, error) {
	return c.SvgToPDF(file, filename, opts...)
}

// Generate calls Generate on the default client.
func Generate(request DetailedHandwritingRequest, opts ...RequestOptions) ([]byte, error) {
	return defaultClient.Generate(request, opts...)
}

// GenerateSimple calls GenerateSimple on the default client.
func GenerateSimple(request SimpleHandwritingRequest, opts ...RequestOptions) ([]byte, error) {
	return defaultClient.GenerateSimple(request, opts...)
}

// SvgToPDF calls SvgToPDF on the default client.
func SvgToPDF(file io.Reader, filename string, opts ...RequestOptions) ([]byte, error) {
	return defaultClient.SvgToPDF(file, filename, opts...)
}

func (c *Client) postJSON(path string, payload interface{}, opts RequestOptions) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	ctx, cancel := contextFor(opts)
	defer cancel()

	req, err := c.newRequest(ctx, http.MethodPost, path, bytes.NewReader(data), "application/json", opts)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient(opts).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, newHandwritingError(resp.StatusCode, body)
	}
	return body, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader, contentType string, opts RequestOptions) (*http.Request, error) {
	base := ""
	if c != nil {
		base = c.BaseURL
	}
	if opts.BaseURL != "" {
		base = opts.BaseURL
	}
	base = NormalizeBaseURL(base)
	if base == "" {
		base = DefaultBaseURL
	}

	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c != nil {
		for key, value := range c.Headers {
			req.Header.Set(key, value)
		}
	}
	for key, value := range opts.Headers {
		req.Header.Set(key, value)
	}
	return req, nil
}

func (c *Client) httpClient(opts RequestOptions) *http.Client {
	if opts.Client != nil {
		return opts.Client
	}
	if c != nil && c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultClient.HTTPClient
}

func contextFor(opts RequestOptions) (context.Context, context.CancelFunc) {
	parent := opts.Context
	if parent == nil {
		parent = context.Background()
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}

func firstOptions(opts []RequestOptions) RequestOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return RequestOptions{}
}

func newHandwritingError(status int, body []byte) *HandwritingError {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}
	return &HandwritingError{
		Message: message,
		Status:  status,
		Body:    body,
	}
}

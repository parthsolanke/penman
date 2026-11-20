# Penman Go client

A small, dependency-free Go client for the Penman handwriting API. It mirrors
the streaming surface of `frontend/src/lib/api.ts` and also wraps the
non-streaming generate and SVG-to-PDF endpoints.

## Installation

The client lives in this repository under `go-client/`:

```
require penman-client v0.0.0

replace penman-client => ../Penman/go-client
```

Then import it:

```go
import penman "penman-client"
```

## Quick start

```go
package main

import (
	"fmt"
	"log"

	penman "penman-client"
)

func main() {
	client := penman.NewClient(penman.DefaultBaseURL)

	svg, err := client.GenerateSimple(penman.SimpleHandwritingRequest{
		TextInput:   "Hello from Go!",
		Style:       0,
		Bias:        1.5,
		StrokeWidth: 2,
		StrokeColor: "#000000",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(svg))
}
```

## Streaming

`StreamHandwriting` reads the server-sent events from
`POST /handwriting/generate-stream` and calls the handlers as paths arrive.

```go
client := penman.NewClient("http://localhost:8000")

err := client.StreamHandwriting(
	penman.HandwritingConfig{
		TextInput:   "Streamed handwriting",
		Style:       0,
		Bias:        1.5,
		StrokeWidth: 2,
		StrokeColor: "#000000",
	},
	penman.StreamHandlers{
		OnSetup: func(setup penman.SetupEvent) {
			fmt.Println("viewBox:", setup.ViewBox)
		},
		OnPath: func(path penman.PathEvent) {
			fmt.Println(path.Data)
		},
		OnError: func(message string) {
			log.Println("stream error:", message)
		},
		OnDone: func() {
			fmt.Println("done")
		},
	},
)
if err != nil {
	log.Fatal(err)
}
```

Pass a context to cancel a stream early:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

go client.StreamHandwriting(config, handlers, penman.RequestOptions{Context: ctx})
```

## Requests and options

Every call accepts optional `penman.RequestOptions`:

```go
svg, err := client.Generate(
	penman.DetailedHandwritingRequest{
		TextInput:    []string{"First line", "Second line"},
		Styles:       []int{0, 4},
		Biases:       []float64{1.5, 1.2},
		StrokeWidths: []int{2, 2},
		StrokeColors: []string{"#000000", "#000000"},
		AsPDF:        false,
	},
	penman.RequestOptions{
		BaseURL: "http://backend:8000",
		Timeout: 60 * time.Second,
		Headers: map[string]string{"X-Request-ID": "demo"},
	},
)
```

Set `AsPDF: true` to receive PDF bytes instead of SVG. Non-2xx responses are
returned as `*penman.HandwritingError` with `Status` and `Body` fields.

## SVG to PDF

```go
f, err := os.Open("drawing.svg")
if err != nil {
	log.Fatal(err)
}
defer f.Close()

pdf, err := client.SvgToPDF(f, "drawing.svg")
```

`ConvertSVGToPDF` is provided as an alias.

## API surface

- `DefaultBaseURL`, `HandwritingPathPrefix`, `DefaultTimeout`
- `Client`, `NewClient`, `CreateClient`, `NormalizeBaseURL`
- `HandwritingConfig`
- `DetailedHandwritingRequest`, `SimpleHandwritingRequest`
- `RequestOptions`, `HandwritingError`
- `Client.Generate`, `Client.GenerateSimple`, `Client.SvgToPDF`,
  `Client.StreamHandwriting`
- Package-level `Generate`, `GenerateSimple`, `SvgToPDF`,
  `StreamHandwriting` using the default client
- `SetupEvent`, `PathEvent`, `StreamHandlers`

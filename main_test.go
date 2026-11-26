package main

import (
	"fmt"
	"testing"
)

// main must serve the telemetry wrapper. The plain server of the framework
// sends no operation in the User-Agent header. When the wrapper gets a new
// name, change the name here too.
func TestMainServesTheTelemetryServer(t *testing.T) {
	const want = "provider.telemetryServer"
	if got := fmt.Sprintf("%T", serverFactory()()); got != want {
		t.Errorf("main serves %s, want %s", got, want)
	}
}

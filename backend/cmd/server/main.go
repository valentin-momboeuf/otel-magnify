// Command server is the otel-magnify community binary. It is a thin
// wrapper around pkg/bootstrap — all bootstrap logic lives there so
// edition-specific binaries can reuse it.
package main

import (
	"context"
	"log"

	"github.com/magnify-labs/otel-magnify/pkg/bootstrap"
)

func main() {
	if err := bootstrap.Run(context.Background(), bootstrap.Options{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}

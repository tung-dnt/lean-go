// Command web is the web server of the site. It does nothing but call server.Run: what the server is made of, and how
// it is shut down, is in internal/server.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/tung-dnt/lean-go/internal/server"
)

func main() {
	if err := server.Run(context.Background(), os.Args, os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

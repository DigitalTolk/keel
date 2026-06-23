// Command keel is the first step in server setup: it prepares a fresh host and
// runs the recurring operations around it, all from a single binary.
package main

import (
	"os"

	"github.com/DigitalTolk/keel/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}

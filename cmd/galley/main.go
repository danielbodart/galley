// galley: a zenity drop-in whose dialogs stack in one window. See the README.
package main

import (
	"os"

	"github.com/danielbodart/galley/internal/client"
)

// Set by the build, from ./VERSION.
var version = "dev"

func main() {
	cwd, _ := os.Getwd()
	os.Exit(client.Main(os.Args, client.Env{
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
		Getenv:  os.Getenv,
		Cwd:     cwd,
		Version: version,
	}))
}

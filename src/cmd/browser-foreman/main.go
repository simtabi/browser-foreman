// Command browser-foreman runs Browser Foreman. Everything lives in internal/cli.
package main

import (
	"os"

	"github.com/simtabi/browser-foreman/src/internal/cli"
)

func main() { os.Exit(cli.Execute()) }

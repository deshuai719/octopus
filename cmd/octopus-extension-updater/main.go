package main

import (
	"os"

	"github.com/bestruirui/octopus/internal/extensionupdater"
)

func main() {
	os.Exit(extensionupdater.Run(os.Args, os.Stdin, os.Stdout))
}

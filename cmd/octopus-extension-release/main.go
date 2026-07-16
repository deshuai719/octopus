package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bestruirui/octopus/internal/extensionupdater"
)

func main() {
	dist := flag.String("dist", "browser-extension/dist", "built extension directory")
	output := flag.String("output", "build/extension-release", "release artifact directory")
	privateKey := flag.String("private-key", "", "Ed25519 PKCS#8 PEM private key")
	flag.Parse()
	if *privateKey == "" {
		fmt.Fprintln(os.Stderr, "--private-key is required")
		os.Exit(2)
	}
	manifest, err := extensionupdater.CreateReleaseArtifacts(*dist, *output, *privateKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("extension-v%s\n", manifest.Version)
}

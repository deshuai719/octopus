package main

import (
	"bytes"
	"os"
	"testing"
)

func TestWindowsApplicationManifestUsesCurrentUser(t *testing.T) {
	manifest, err := os.ReadFile("octopus-extension-updater.manifest")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range [][]byte{
		[]byte(`requestedExecutionLevel level="asInvoker"`),
		[]byte(`uiAccess="false"`),
	} {
		if !bytes.Contains(manifest, required) {
			t.Fatalf("application manifest is missing %q", required)
		}
	}

	resource, err := os.ReadFile("rsrc_windows_amd64.syso")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(resource, []byte("asInvoker")) {
		t.Fatal("Windows resource does not embed the asInvoker application manifest")
	}
}

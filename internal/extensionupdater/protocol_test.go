package extensionupdater

import (
	"bytes"
	"testing"
)

func TestNativeMessageRoundTrip(t *testing.T) {
	t.Parallel()
	want := Request{ProtocolVersion: 1, RequestID: "test", Action: "status", CurrentVersion: "0.2.0"}
	var buffer bytes.Buffer
	if err := WriteMessage(&buffer, want); err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := ReadMessage(&buffer, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

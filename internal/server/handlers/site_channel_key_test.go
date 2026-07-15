package handlers

import (
	"errors"
	"net/http"
	"testing"
)

func TestSiteChannelMutationErrorStatusTreatsCapabilityReasonsAsBadRequest(t *testing.T) {
	for _, message := range []string{
		"group_binding_not_supported: One API cannot bind a group",
		"credential_api_key_read_only: management write is unavailable",
	} {
		if got := siteChannelMutationErrorStatus(errors.New(message)); got != http.StatusBadRequest {
			t.Fatalf("status for %q = %d, want %d", message, got, http.StatusBadRequest)
		}
	}
}

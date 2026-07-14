package grouphealth

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestBuildProbeRequestForResponses(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "gpt-5.4")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/responses" {
		t.Fatalf("expected /v1/responses, got %s", req.URL.Path)
	}
}

func TestBuildProbeRequestUsesRepresentativePublicPrompt(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequestWithProfile(
		context.Background(),
		channel,
		usedKey,
		"gpt-4o-mini",
		model.GroupHealthProbeProfilePublicCompat,
	)
	if err != nil {
		t.Fatalf("buildProbeRequestWithProfile returned error: %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "连接正常") {
		t.Fatalf("expected representative prompt, got %s", text)
	}
	if !strings.Contains(text, `"max_tokens":32`) {
		t.Fatalf("expected 32 output tokens, got %s", text)
	}
}

func TestSelectProbeProfileKeepsEmbeddingsMinimal(t *testing.T) {
	profile := selectProbeProfile([]string{model.SiteTagPublic}, outbound.OutboundTypeOpenAIEmbedding)
	if profile != model.GroupHealthProbeProfileStandard {
		t.Fatalf("embedding profile = %s, want standard", profile)
	}
}

func TestBuildProbeRequestForEmbeddings(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIEmbedding,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "text-embedding-3-large")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/embeddings" {
		t.Fatalf("expected /v1/embeddings, got %s", req.URL.Path)
	}
}

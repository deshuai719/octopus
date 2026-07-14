package grouphealth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/helper"
	"github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

type ProbeResult struct {
	Success      bool
	HTTPStatus   int
	DurationMS   int64
	ErrorMessage string
	Header       http.Header // 上游响应头，供 POR 门3 做 Cloudflare 指纹识别
}

type Prober struct {
	CandidateTimeout       time.Duration
	PublicCandidateTimeout time.Duration
}

func NewProber() *Prober {
	return &Prober{
		CandidateTimeout:       12 * time.Second,
		PublicCandidateTimeout: 45 * time.Second,
	}
}

func (p *Prober) RunCandidate(ctx context.Context, channel model.Channel, usedKey model.ChannelKey, modelName string) ProbeResult {
	return p.RunCandidateWithProfile(ctx, channel, usedKey, modelName, model.GroupHealthProbeProfileStandard)
}

func (p *Prober) RunCandidateWithProfile(ctx context.Context, channel model.Channel, usedKey model.ChannelKey, modelName string, profile model.GroupHealthProbeProfile) ProbeResult {
	startedAt := time.Now()
	result := ProbeResult{}

	timeout := p.CandidateTimeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	if profile == model.GroupHealthProbeProfilePublicCompat {
		timeout = p.PublicCandidateTimeout
		if timeout <= 0 {
			timeout = 45 * time.Second
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request, err := buildProbeRequestWithProfile(probeCtx, &channel, &usedKey, modelName, profile)
	if err != nil {
		result.ErrorMessage = err.Error()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	applyCustomHeaders(request, channel.CustomHeader)
	// 防止 Go 默认 User-Agent 泄露到上游
	if request.Header.Get("User-Agent") == "" {
		request.Header.Set("User-Agent", "")
	}
	if err := helper.ApplyParamOverride(request, channel.ParamOverride); err != nil {
		result.ErrorMessage = err.Error()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	httpClient, err := helper.ChannelHTTPClientWithContext(probeCtx, &channel)
	if err != nil {
		result.ErrorMessage = err.Error()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}

	response, err := httpClient.Do(request)
	if err != nil {
		result.ErrorMessage = err.Error()
		result.DurationMS = time.Since(startedAt).Milliseconds()
		return result
	}
	defer response.Body.Close()

	result.HTTPStatus = response.StatusCode
	result.Header = response.Header.Clone()
	result.DurationMS = time.Since(startedAt).Milliseconds()

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		result.Success = true
		return result
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
	if len(body) > 0 {
		result.ErrorMessage = fmt.Sprintf("upstream error: %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	} else {
		result.ErrorMessage = fmt.Sprintf("upstream error: %d", response.StatusCode)
	}
	return result
}

func buildProbeRequest(ctx context.Context, channel *model.Channel, usedKey *model.ChannelKey, modelName string) (*http.Request, error) {
	return buildProbeRequestWithProfile(ctx, channel, usedKey, modelName, model.GroupHealthProbeProfileStandard)
}

func buildProbeRequestWithProfile(ctx context.Context, channel *model.Channel, usedKey *model.ChannelKey, modelName string, profile model.GroupHealthProbeProfile) (*http.Request, error) {
	if channel == nil {
		return nil, fmt.Errorf("channel is nil")
	}
	if usedKey == nil {
		return nil, fmt.Errorf("channel key is nil")
	}
	if strings.TrimSpace(usedKey.ChannelKey) == "" {
		return nil, fmt.Errorf("channel key is empty")
	}
	if strings.TrimSpace(modelName) == "" {
		return nil, fmt.Errorf("model name is empty")
	}

	request := buildProbeInternalRequest(channel.Type, modelName, profile)
	adapter := outbound.Get(channel.Type)
	if adapter == nil {
		return nil, fmt.Errorf("unsupported outbound type: %d", channel.Type)
	}
	return adapter.TransformRequest(ctx, request, channel.GetBaseUrl(), usedKey.ChannelKey)
}

func buildProbeInternalRequest(channelType outbound.OutboundType, modelName string, profile model.GroupHealthProbeProfile) *transformerModel.InternalLLMRequest {
	stream := false
	ping := "ping"
	prompt := ping
	maxTokens := int64(1)
	if profile == model.GroupHealthProbeProfilePublicCompat && channelType != outbound.OutboundTypeOpenAIEmbedding {
		prompt = "请简短回答：你能正常收到并回复这条消息吗？如果可以，只回答“连接正常”。"
		maxTokens = 32
	}

	switch channelType {
	case outbound.OutboundTypeOpenAIEmbedding:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatOpenAIEmbedding,
			EmbeddingInput: &transformerModel.EmbeddingInput{
				Single: &ping,
			},
		}
	case outbound.OutboundTypeOpenAIResponse:
		return &transformerModel.InternalLLMRequest{
			Model:               modelName,
			RawAPIFormat:        transformerModel.APIFormatOpenAIResponse,
			Messages:            []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			Stream:              &stream,
			MaxCompletionTokens: &maxTokens,
		}
	case outbound.OutboundTypeAnthropic:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatAnthropicMessage,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			Stream:       &stream,
			MaxTokens:    &maxTokens,
		}
	case outbound.OutboundTypeGemini:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatGeminiContents,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			Stream:       &stream,
			MaxTokens:    &maxTokens,
		}
	case outbound.OutboundTypeVolcengine:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatOpenAIChatCompletion,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			Stream:       &stream,
			MaxTokens:    &maxTokens,
		}
	default:
		return &transformerModel.InternalLLMRequest{
			Model:        modelName,
			RawAPIFormat: transformerModel.APIFormatOpenAIChatCompletion,
			Messages:     []transformerModel.Message{{Role: "user", Content: transformerModel.MessageContent{Content: &prompt}}},
			Stream:       &stream,
			MaxTokens:    &maxTokens,
		}
	}
}

func selectProbeProfile(tags []string, channelType outbound.OutboundType) model.GroupHealthProbeProfile {
	if channelType == outbound.OutboundTypeOpenAIEmbedding {
		return model.GroupHealthProbeProfileStandard
	}
	for _, tag := range tags {
		if tag == model.SiteTagPublic {
			return model.GroupHealthProbeProfilePublicCompat
		}
	}
	return model.GroupHealthProbeProfileStandard
}

func applyCustomHeaders(request *http.Request, headers []model.CustomHeader) {
	if request == nil {
		return
	}
	for _, header := range headers {
		key := strings.TrimSpace(header.HeaderKey)
		if key == "" {
			continue
		}
		request.Header.Set(key, header.HeaderValue)
	}
}

// Package translator_test provides end-to-end request and response translation test coverage.
package translator_test

import (
	"flamerouter/internal/translator"
	"flamerouter/internal/translator/concerns"
	"reflect"
	"testing"

	_ "flamerouter/internal/translator/request"
	_ "flamerouter/internal/translator/response"
)

type requestTestCase struct {
	validate     func(t *testing.T, res map[string]any)
	body         map[string]any
	name         string
	sourceFormat string
	targetFormat string
	opts         translator.TranslateOptions
}

func getIdentityTestCase() requestTestCase {
	return requestTestCase{
		name:         "Identity Translation",
		sourceFormat: translator.FormatOpenAI,
		targetFormat: translator.FormatOpenAI,
		body: map[string]any{
			"model": "gpt-4o",
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "Hello",
				},
			},
		},
		opts: translator.TranslateOptions{
			ClientTool:   nil,
			Credentials:  nil,
			Model:        "gpt-4o",
			Provider:     "openai",
			ConnectionID: "conn-123",
			StripList:    nil,
			Stream:       false,
		},
		validate: func(t *testing.T, res map[string]any) {
			t.Helper()
			if res == nil {
				t.Fatalf("expected non-nil response")
			}
			if res["model"] != "gpt-4o" {
				t.Errorf("expected model gpt-4o, got %v", res["model"])
			}
		},
	}
}

func getOpenAIToClaudeTestCase() requestTestCase {
	return requestTestCase{
		name:         "OpenAI to Claude Translation",
		sourceFormat: translator.FormatOpenAI,
		targetFormat: translator.FormatClaude,
		body: map[string]any{
			"model": "claude-3-5-sonnet-20241022",
			"messages": []any{
				map[string]any{
					"role":    "system",
					"content": "You are a helpful assistant.",
				},
				map[string]any{
					"role":    "user",
					"content": "Tell me a joke.",
				},
			},
			"max_tokens": 1024,
		},
		opts: translator.TranslateOptions{
			ClientTool:   nil,
			Credentials:  nil,
			Model:        "claude-3-5-sonnet-20241022",
			Provider:     "claude",
			ConnectionID: "conn-claude",
			StripList:    nil,
			Stream:       false,
		},
		validate: func(t *testing.T, res map[string]any) {
			t.Helper()
			if res == nil {
				t.Fatalf("expected non-nil response")
			}
			if _, ok := res["messages"].([]any); !ok {
				t.Errorf("expected Claude messages slice, got %T", res["messages"])
			}
			if sys, ok := res["system"].([]any); ok {
				if len(sys) == 0 {
					t.Errorf("expected non-empty system prompt array")
				}
			}
		},
	}
}

func getClaudeToOpenAITestCase() requestTestCase {
	return requestTestCase{
		name:         "Claude to OpenAI Translation",
		sourceFormat: translator.FormatClaude,
		targetFormat: translator.FormatOpenAI,
		body: map[string]any{
			"model": "claude-3-5-sonnet-20241022",
			"system": []any{
				map[string]any{
					"type": "text",
					"text": "Act as a math tutor.",
				},
			},
			"messages": []any{
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{
							"type": "text",
							"text": "What is 2 + 2?",
						},
					},
				},
			},
		},
		opts: translator.TranslateOptions{
			ClientTool:   nil,
			Credentials:  nil,
			Model:        "gpt-4o",
			Provider:     "openai",
			ConnectionID: "conn-openai",
			StripList:    nil,
			Stream:       false,
		},
		validate: func(t *testing.T, res map[string]any) {
			t.Helper()
			if res == nil {
				t.Fatalf("expected non-nil response")
			}
			msgs, ok := res["messages"].([]any)
			if !ok || len(msgs) == 0 {
				t.Fatalf("expected OpenAI messages slice, got %v", res["messages"])
			}
			firstMsg, okMsg := msgs[0].(map[string]any)
			if !okMsg {
				t.Fatalf("expected first message to be map[string]any")
			}
			if firstMsg["role"] != "system" {
				t.Errorf("expected first message to be system, got %v", firstMsg["role"])
			}
		},
	}
}

func getOpenAIToGeminiTestCase() requestTestCase {
	return requestTestCase{
		name:         "OpenAI to Gemini Translation",
		sourceFormat: translator.FormatOpenAI,
		targetFormat: translator.FormatGemini,
		body: map[string]any{
			"model": "gemini-1.5-pro",
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "Explain quantum computing in one sentence.",
				},
			},
		},
		opts: translator.TranslateOptions{
			ClientTool:   nil,
			Credentials:  nil,
			Model:        "gemini-1.5-pro",
			Provider:     "gemini",
			ConnectionID: "conn-gemini",
			StripList:    nil,
			Stream:       false,
		},
		validate: func(t *testing.T, res map[string]any) {
			t.Helper()
			if res == nil {
				t.Fatalf("expected non-nil response")
			}
			if _, ok := res["contents"].([]any); !ok {
				t.Errorf("expected Gemini contents slice, got %T", res["contents"])
			}
		},
	}
}

func TestTranslateRequest_EndToEnd(t *testing.T) {
	t.Parallel()

	tests := []requestTestCase{
		getIdentityTestCase(),
		getOpenAIToClaudeTestCase(),
		getClaudeToOpenAITestCase(),
		getOpenAIToGeminiTestCase(),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := translator.DefaultRegistry.TranslateRequest(tt.sourceFormat, tt.targetFormat, tt.body, tt.opts)
			tt.validate(t, res)
		})
	}
}

func newTestResponseState() *concerns.ResponseState {
	return &concerns.ResponseState{
		FuncNames:            make(map[int]string),
		ToolCallAccum:        make(map[int]map[string]any),
		KiroToolCalls:        make(map[int]map[string]any),
		RawUsage:             make(map[string]any),
		ToolIndexByID:        make(map[string]int),
		FuncArgsBuf:          make(map[int]string),
		ToolCalls:            make(map[int]*concerns.ToolCallInfo),
		ToolArgBuffers:       make(map[int]string),
		Usage:                nil,
		FuncCallIDs:          make(map[int]string),
		ToolNameMap:          make(map[string]string),
		Model:                "gpt-4o",
		ResponseID:           "resp-1",
		ReasoningID:          "",
		MessageTextBuf:       "",
		AccumulatedThinking:  "",
		AccumulatedContent:   "",
		MessageID:            "msg-1",
		FinishReason:         "",
		Created:              1234567890,
		ToolCallIndex:        0,
		ServerToolBlockIndex: 0,
		ChunkIndex:           0,
		ToolIndex:            0,
		CurrentBlockIndex:    0,
		ThinkingBlockIndex:   0,
		ContentBlockIndex:    0,
		TextBlockIndex:       0,
		NextBlockIndex:       0,
		OutputIndex:          0,
		FuncOutputIndex:      0,
		ReasoningDone:        false,
		ResponseCreated:      false,
		MessageStartSent:     false,
		MessageStarted:       false,
		ReasoningStarted:     false,
		ResponsesStarted:     false,
		TextBlockClosed:      false,
		FinishReasonSent:     false,
		HadToolCalls:         false,
		HadToolUse:           false,
		InThinkingBlock:      false,
		ThinkingBlockStarted: false,
		TextBlockStarted:     false,
	}
}

func TestTranslateResponse_Identity(t *testing.T) {
	t.Parallel()

	state := newTestResponseState()
	chunk := map[string]any{
		"id": "chatcmpl-123",
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"content": "Hello world",
				},
			},
		},
	}

	res := translator.DefaultRegistry.TranslateResponse(translator.FormatOpenAI, translator.FormatOpenAI, chunk, state)
	if len(res) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(res))
	}

	if !reflect.DeepEqual(res[0], chunk) {
		t.Errorf("expected identity match")
	}
}

func TestTranslateResponse_ClaudeToOpenAI(t *testing.T) {
	t.Parallel()

	state := newTestResponseState()
	claudeChunk := map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{
			"type": "text_delta",
			"text": "Streaming Claude response",
		},
	}

	claudeRes := translator.DefaultRegistry.TranslateResponse(translator.FormatClaude, translator.FormatOpenAI, claudeChunk, state)
	if len(claudeRes) == 0 {
		t.Fatalf("expected translated response chunk(s), got 0")
	}
}

package concerns

// ResponseState tracks state during stream translation across chunks.
type ResponseState struct {
	FuncNames            map[int]string
	ToolCallAccum        map[int]map[string]any
	KiroToolCalls        map[int]map[string]any
	RawUsage             map[string]any
	ToolIndexByID        map[string]int
	FuncArgsBuf          map[int]string
	ToolCalls            map[int]*ToolCallInfo
	ToolArgBuffers       map[int]string
	Usage                *UsageInfo
	FuncCallIDs          map[int]string
	ToolNameMap          map[string]string
	Model                string
	ResponseID           string
	ReasoningID          string
	MessageTextBuf       string
	AccumulatedThinking  string
	AccumulatedContent   string
	MessageID            string
	FinishReason         string
	Created              int64
	ToolCallIndex        int
	ServerToolBlockIndex int
	ChunkIndex           int
	ToolIndex            int
	CurrentBlockIndex    int
	ThinkingBlockIndex   int
	ContentBlockIndex    int
	TextBlockIndex       int
	NextBlockIndex       int
	OutputIndex          int
	FuncOutputIndex      int
	ReasoningDone        bool
	ResponseCreated      bool
	MessageStartSent     bool
	MessageStarted       bool
	ReasoningStarted     bool
	ResponsesStarted     bool
	TextBlockClosed      bool
	FinishReasonSent     bool
	HadToolCalls         bool
	HadToolUse           bool
	InThinkingBlock      bool
	ThinkingBlockStarted bool
	TextBlockStarted     bool
}

// ToolCallInfo tracks info about a tool call block.
type ToolCallInfo struct {
	ID         string
	Name       string
	BlockIndex int
}

// UsageInfo holds token usage counts.
type UsageInfo struct {
	PromptTokens             int
	CompletionTokens         int
	TotalTokens              int
	InputTokens              int
	OutputTokens             int
	CacheReadTokens          int
	CacheCreateTokens        int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

// NewResponseState initializes and returns a new ResponseState.
func NewResponseState() *ResponseState {
	return &ResponseState{
		FuncNames:            nil,
		ToolCallAccum:        nil,
		KiroToolCalls:        nil,
		RawUsage:             nil,
		ToolIndexByID:        nil,
		FuncArgsBuf:          nil,
		ToolCalls:            make(map[int]*ToolCallInfo),
		ToolArgBuffers:       make(map[int]string),
		Usage:                nil,
		FuncCallIDs:          nil,
		ToolNameMap:          nil,
		Model:                "",
		ResponseID:           "",
		ReasoningID:          "",
		MessageTextBuf:       "",
		AccumulatedThinking:  "",
		AccumulatedContent:   "",
		MessageID:            "",
		FinishReason:         "",
		Created:              0,
		ToolCallIndex:        0,
		ServerToolBlockIndex: -1,
		ChunkIndex:           0,
		ToolIndex:            0,
		CurrentBlockIndex:    0,
		ThinkingBlockIndex:   0,
		ContentBlockIndex:    -1,
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

// StreamChunk models an OpenAI stream completion chunk.
type StreamChunk struct {
	Usage   *UsageInfo    `json:"usage,omitempty"`
	ID      string        `json:"id,omitempty"`
	Object  string        `json:"object,omitempty"`
	Model   string        `json:"model,omitempty"`
	Choices []ChoiceChunk `json:"choices,omitempty"`
	Created int64         `json:"created,omitempty"`
}

// ChoiceChunk models choices within a stream chunk.
type ChoiceChunk struct {
	Delta        map[string]any `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason,omitempty"`
	Index        int            `json:"index"`
}

// BuildChunk constructs a StreamChunk.
func BuildChunk(id string, created int64, model string, delta map[string]any, finishReason *string) StreamChunk {
	return StreamChunk{
		Usage:   nil,
		ID:      id,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChoiceChunk{
			{
				Index:        0,
				Delta:        delta,
				FinishReason: finishReason,
			},
		},
	}
}

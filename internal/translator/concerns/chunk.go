package concerns

type ResponseState struct {
	FuncNames            map[int]string
	ToolCallAccum        map[int]map[string]any
	KiroToolCalls        map[int]map[string]any
	RawUsage             map[string]any
	ToolIndexById        map[string]int
	FuncArgsBuf          map[int]string
	ToolCalls            map[int]*ToolCallInfo
	ToolArgBuffers       map[int]string
	Usage                *UsageInfo
	FuncCallIds          map[int]string
	ToolNameMap          map[string]string
	Model                string
	ResponseId           string
	ReasoningId          string
	MessageTextBuf       string
	AccumulatedThinking  string
	AccumulatedContent   string
	MessageID            string
	MessageId            string
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

type ToolCallInfo struct {
	ID         string
	Name       string
	BlockIndex int
}

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

func NewResponseState() *ResponseState {
	return &ResponseState{
		ToolCalls:            make(map[int]*ToolCallInfo),
		ToolArgBuffers:       make(map[int]string),
		NextBlockIndex:       0,
		ContentBlockIndex:    -1,
		ServerToolBlockIndex: -1,
	}
}

type StreamChunk struct {
	Usage   *UsageInfo    `json:"usage,omitempty"`
	ID      string        `json:"id,omitempty"`
	Object  string        `json:"object,omitempty"`
	Model   string        `json:"model,omitempty"`
	Choices []ChoiceChunk `json:"choices,omitempty"`
	Created int64         `json:"created,omitempty"`
}

type ChoiceChunk struct {
	Delta        map[string]any `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason,omitempty"`
	Index        int            `json:"index"`
}

func BuildChunk(id string, created int64, model string, delta map[string]any, finishReason *string) StreamChunk {
	return StreamChunk{
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

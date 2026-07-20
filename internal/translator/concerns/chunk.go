package concerns

type ResponseState struct {
	MessageID            string
	Model                string
	TextBlockStarted     bool
	ThinkingBlockStarted bool
	InThinkingBlock      bool
	CurrentBlockIndex    int
	ToolCalls            map[int]*ToolCallInfo
	FinishReason         string
	FinishReasonSent     bool
	Usage                *UsageInfo
	ContentBlockIndex    int
	NextBlockIndex       int
	ToolCallIndex        int
	MessageStartSent     bool
	TextBlockIndex       int
	TextBlockClosed      bool
	ThinkingBlockIndex   int
	ServerToolBlockIndex int
	ToolNameMap          map[string]string
	ToolArgBuffers       map[int]string

	ResponseId      string
	ResponseCreated bool
	Created         int64
	ChunkIndex      int
	HadToolUse      bool
	HadToolCalls    bool

	AccumulatedContent  string
	AccumulatedThinking string

	ToolCallAccum map[int]map[string]any

	ResponsesStarted bool
	OutputIndex      int
	FuncOutputIndex  int
	ReasoningStarted bool
	ReasoningDone    bool
	ReasoningId      string
	MessageStarted   bool
	MessageId        string
	MessageTextBuf   string
	FuncNames        map[int]string
	FuncCallIds      map[int]string
	FuncArgsBuf      map[int]string
	ToolIndex        int
	ToolIndexById    map[string]int
	RawUsage         map[string]any
	KiroToolCalls    map[int]map[string]any
}

type ToolCallInfo struct {
	ID         string
	Name       string
	BlockIndex int
}

type UsageInfo struct {
	PromptTokens              int
	CompletionTokens          int
	TotalTokens               int
	InputTokens               int
	OutputTokens              int
	CacheReadTokens           int
	CacheCreateTokens         int
	CacheReadInputTokens      int
	CacheCreationInputTokens  int
}

func NewResponseState() *ResponseState {
	return &ResponseState{
		ToolCalls:          make(map[int]*ToolCallInfo),
		ToolArgBuffers:     make(map[int]string),
		NextBlockIndex:     0,
		ContentBlockIndex:  -1,
		ServerToolBlockIndex: -1,
	}
}

type StreamChunk struct {
	ID      string         `json:"id,omitempty"`
	Object  string         `json:"object,omitempty"`
	Created int64          `json:"created,omitempty"`
	Model   string         `json:"model,omitempty"`
	Choices []ChoiceChunk  `json:"choices,omitempty"`
	Usage   *UsageInfo     `json:"usage,omitempty"`
}

type ChoiceChunk struct {
	Index        int            `json:"index"`
	Delta        map[string]any `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason,omitempty"`
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

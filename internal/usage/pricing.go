package usage

import (
	"math"
	"regexp"
	"strings"
)

// ModelPricing defines token rates in USD per 1M tokens.
type ModelPricing struct {
	Input         float64
	Output        float64
	Cached        float64
	Reasoning     float64
	CacheCreation float64
}

// Canonical model pricing table ($ / 1M tokens).
var modelPricingTable = map[string]ModelPricing{
	// Anthropic / Claude
	"claude-opus-4-6":            {Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 25.00, CacheCreation: 6.25},
	"claude-opus-4-5-20251101":   {Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 25.00, CacheCreation: 6.25},
	"claude-sonnet-4-6":          {Input: 3.00, Output: 15.00, Cached: 0.30, Reasoning: 15.00, CacheCreation: 3.75},
	"claude-sonnet-4-5-20250929": {Input: 3.00, Output: 15.00, Cached: 0.30, Reasoning: 15.00, CacheCreation: 3.75},
	"claude-haiku-4-5-20251001":  {Input: 1.00, Output: 5.00, Cached: 0.10, Reasoning: 5.00, CacheCreation: 1.25},
	"claude-sonnet-4-20250514":   {Input: 3.00, Output: 15.00, Cached: 1.50, Reasoning: 15.00, CacheCreation: 3.00},
	"claude-opus-4-20250514":     {Input: 15.00, Output: 25.00, Cached: 7.50, Reasoning: 112.50, CacheCreation: 15.00},
	"claude-3-5-sonnet-20241022": {Input: 3.00, Output: 15.00, Cached: 1.50, Reasoning: 15.00, CacheCreation: 3.00},
	"claude-haiku-4.5":           {Input: 0.50, Output: 2.50, Cached: 0.05, Reasoning: 3.75, CacheCreation: 0.50},
	"claude-opus-4.1":            {Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 37.50, CacheCreation: 5.00},
	"claude-opus-4.5":            {Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 37.50, CacheCreation: 5.00},
	"claude-opus-4.6":            {Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 37.50, CacheCreation: 5.00},
	"claude-sonnet-4":            {Input: 3.00, Output: 15.00, Cached: 0.30, Reasoning: 22.50, CacheCreation: 3.00},
	"claude-sonnet-4.5":          {Input: 3.00, Output: 15.00, Cached: 0.30, Reasoning: 22.50, CacheCreation: 3.00},
	"claude-sonnet-4.6":          {Input: 3.00, Output: 15.00, Cached: 0.30, Reasoning: 22.50, CacheCreation: 3.00},
	"claude-opus-4-5-thinking":   {Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 37.50, CacheCreation: 5.00},
	"claude-opus-4-6-thinking":   {Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 37.50, CacheCreation: 5.00},
	"claude-fable-5":             {Input: 10.00, Output: 50.00, Cached: 1.00, Reasoning: 50.00, CacheCreation: 12.50},

	// OpenAI / GPT
	"gpt-3.5-turbo":           {Input: 0.50, Output: 1.50, Cached: 0.25, Reasoning: 2.25, CacheCreation: 0.50},
	"gpt-4":                   {Input: 2.50, Output: 10.00, Cached: 1.25, Reasoning: 15.00, CacheCreation: 2.50},
	"gpt-4-turbo":             {Input: 10.00, Output: 30.00, Cached: 5.00, Reasoning: 45.00, CacheCreation: 10.00},
	"gpt-4o":                  {Input: 2.50, Output: 10.00, Cached: 1.25, Reasoning: 15.00, CacheCreation: 2.50},
	"gpt-4o-mini":             {Input: 0.15, Output: 0.60, Cached: 0.075, Reasoning: 0.90, CacheCreation: 0.15},
	"gpt-4.1":                 {Input: 2.50, Output: 10.00, Cached: 1.25, Reasoning: 15.00, CacheCreation: 2.50},
	"gpt-5":                   {Input: 1.25, Output: 10.00, Cached: 0.625, Reasoning: 10.00, CacheCreation: 1.25},
	"gpt-5-mini":              {Input: 0.25, Output: 2.00, Cached: 0.125, Reasoning: 2.00, CacheCreation: 0.25},
	"gpt-5-codex":             {Input: 1.25, Output: 10.00, Cached: 0.625, Reasoning: 10.00, CacheCreation: 1.25},
	"gpt-5.1":                 {Input: 1.25, Output: 10.00, Cached: 0.625, Reasoning: 10.00, CacheCreation: 1.25},
	"gpt-5.1-codex":           {Input: 1.25, Output: 10.00, Cached: 0.625, Reasoning: 10.00, CacheCreation: 1.25},
	"gpt-5.1-codex-mini":      {Input: 1.50, Output: 6.00, Cached: 0.75, Reasoning: 9.00, CacheCreation: 1.50},
	"gpt-5.1-codex-mini-high": {Input: 2.00, Output: 8.00, Cached: 1.00, Reasoning: 12.00, CacheCreation: 2.00},
	"gpt-5.1-codex-max":       {Input: 8.00, Output: 32.00, Cached: 4.00, Reasoning: 48.00, CacheCreation: 8.00},
	"gpt-5.2":                 {Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75},
	"gpt-5.2-codex":           {Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75},
	"gpt-5.3-codex":           {Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75},
	"gpt-5.3-codex-spark":     {Input: 3.00, Output: 12.00, Cached: 0.30, Reasoning: 12.00, CacheCreation: 3.00},
	"gpt-5.6":                 {Input: 2.50, Output: 15.00, Cached: 0.25, Reasoning: 15.00, CacheCreation: 2.50},
	"gpt-5.6-luna":            {Input: 1.00, Output: 6.00, Cached: 0.10, Reasoning: 6.00, CacheCreation: 1.00},
	"gpt-5.6-terra":           {Input: 2.50, Output: 15.00, Cached: 0.25, Reasoning: 15.00, CacheCreation: 2.50},
	"gpt-5.6-sol":             {Input: 5.00, Output: 30.00, Cached: 0.50, Reasoning: 30.00, CacheCreation: 5.00},
	"o1":                      {Input: 15.00, Output: 60.00, Cached: 7.50, Reasoning: 90.00, CacheCreation: 15.00},
	"o1-mini":                 {Input: 3.00, Output: 12.00, Cached: 1.50, Reasoning: 18.00, CacheCreation: 3.00},

	// Gemini
	"gemini-3.7-flash":           {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.7-flash-high":      {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.7-flash-medium":    {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.7-flash-low":       {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.6-flash":           {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.6-flash-high":      {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.6-flash-medium":    {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.6-flash-low":       {Input: 1.50, Output: 7.50, Cached: 0.15, Reasoning: 11.25, CacheCreation: 1.875},
	"gemini-3.5-flash-lite":      {Input: 0.30, Output: 2.50, Cached: 0.03, Reasoning: 3.75, CacheCreation: 0.375},
	"gemini-3.5-flash-high":      {Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50},
	"gemini-3-flash-preview":     {Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50},
	"gemini-3-pro-preview":       {Input: 2.00, Output: 12.00, Cached: 0.25, Reasoning: 18.00, CacheCreation: 2.00},
	"gemini-3.1-pro-low":         {Input: 2.00, Output: 12.00, Cached: 0.25, Reasoning: 18.00, CacheCreation: 2.00},
	"gemini-3.1-pro-high":        {Input: 4.00, Output: 18.00, Cached: 0.50, Reasoning: 27.00, CacheCreation: 4.00},
	"gemini-pro-agent":           {Input: 4.00, Output: 18.00, Cached: 0.50, Reasoning: 27.00, CacheCreation: 4.00},
	"gemini-3-flash-agent":       {Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50},
	"gemini-3.5-flash-low":       {Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50},
	"gemini-3.5-flash-extra-low": {Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50},
	"gemini-3-flash":             {Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50},
	"gemini-2.5-pro":             {Input: 2.00, Output: 12.00, Cached: 0.25, Reasoning: 18.00, CacheCreation: 2.00},
	"gemini-2.5-flash":           {Input: 0.30, Output: 2.50, Cached: 0.03, Reasoning: 3.75, CacheCreation: 0.30},
	"gemini-2.5-flash-lite":      {Input: 0.15, Output: 1.25, Cached: 0.015, Reasoning: 1.875, CacheCreation: 0.15},

	// DeepSeek
	"deepseek-chat":      {Input: 0.14, Output: 0.28, Cached: 0.0028, Reasoning: 0.28, CacheCreation: 0.14},
	"deepseek-reasoner":  {Input: 0.14, Output: 0.28, Cached: 0.0028, Reasoning: 0.28, CacheCreation: 0.14},
	"deepseek-r1":        {Input: 0.14, Output: 0.28, Cached: 0.0028, Reasoning: 0.28, CacheCreation: 0.14},
	"deepseek-v3.2-chat": {Input: 0.14, Output: 0.28, Cached: 0.0028, Reasoning: 0.28, CacheCreation: 0.14},
}

type patternPricingEntry struct {
	pattern string
	pricing ModelPricing
}

var patternPricingList = []patternPricingEntry{
	{pattern: "*-codex-xhigh", pricing: ModelPricing{Input: 10.00, Output: 40.00, Cached: 5.00, Reasoning: 60.00, CacheCreation: 10.00}},
	{pattern: "*-codex-high", pricing: ModelPricing{Input: 8.00, Output: 32.00, Cached: 4.00, Reasoning: 48.00, CacheCreation: 8.00}},
	{pattern: "*-codex-max", pricing: ModelPricing{Input: 8.00, Output: 32.00, Cached: 4.00, Reasoning: 48.00, CacheCreation: 8.00}},
	{pattern: "*-codex-mini-*", pricing: ModelPricing{Input: 1.50, Output: 6.00, Cached: 0.75, Reasoning: 9.00, CacheCreation: 1.50}},
	{pattern: "*-codex-mini", pricing: ModelPricing{Input: 1.50, Output: 6.00, Cached: 0.75, Reasoning: 9.00, CacheCreation: 1.50}},
	{pattern: "*-codex-low", pricing: ModelPricing{Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75}},
	{pattern: "*-codex-spark", pricing: ModelPricing{Input: 3.00, Output: 12.00, Cached: 0.30, Reasoning: 12.00, CacheCreation: 3.00}},
	{pattern: "codex-*", pricing: ModelPricing{Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75}},
	{pattern: "*-codex", pricing: ModelPricing{Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75}},
	{pattern: "claude-opus-*", pricing: ModelPricing{Input: 5.00, Output: 25.00, Cached: 0.50, Reasoning: 25.00, CacheCreation: 6.25}},
	{pattern: "claude-sonnet-*", pricing: ModelPricing{Input: 3.00, Output: 15.00, Cached: 0.30, Reasoning: 15.00, CacheCreation: 3.75}},
	{pattern: "claude-haiku-*", pricing: ModelPricing{Input: 1.00, Output: 5.00, Cached: 0.10, Reasoning: 5.00, CacheCreation: 1.25}},
	{pattern: "claude-*", pricing: ModelPricing{Input: 3.00, Output: 15.00, Cached: 0.30, Reasoning: 15.00, CacheCreation: 3.75}},
	{pattern: "gemini-*-flash-lite", pricing: ModelPricing{Input: 0.15, Output: 1.25, Cached: 0.015, Reasoning: 1.875, CacheCreation: 0.15}},
	{pattern: "gemini-*-flash", pricing: ModelPricing{Input: 0.30, Output: 2.50, Cached: 0.03, Reasoning: 3.75, CacheCreation: 0.30}},
	{pattern: "gemini-*-pro", pricing: ModelPricing{Input: 2.00, Output: 12.00, Cached: 0.25, Reasoning: 18.00, CacheCreation: 2.00}},
	{pattern: "gemini-3-*", pricing: ModelPricing{Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50}},
	{pattern: "gemini-2.5-*", pricing: ModelPricing{Input: 0.30, Output: 2.50, Cached: 0.03, Reasoning: 3.75, CacheCreation: 0.30}},
	{pattern: "gemini-*", pricing: ModelPricing{Input: 0.50, Output: 3.00, Cached: 0.03, Reasoning: 4.50, CacheCreation: 0.50}},
	{pattern: "gpt-5.6-*", pricing: ModelPricing{Input: 2.50, Output: 15.00, Cached: 0.25, Reasoning: 15.00, CacheCreation: 2.50}},
	{pattern: "gpt-5.3-*", pricing: ModelPricing{Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75}},
	{pattern: "gpt-5.2-*", pricing: ModelPricing{Input: 1.75, Output: 14.00, Cached: 0.175, Reasoning: 14.00, CacheCreation: 1.75}},
	{pattern: "gpt-5.1-*", pricing: ModelPricing{Input: 1.25, Output: 10.00, Cached: 0.625, Reasoning: 10.00, CacheCreation: 1.25}},
	{pattern: "gpt-5-*", pricing: ModelPricing{Input: 1.25, Output: 10.00, Cached: 0.625, Reasoning: 10.00, CacheCreation: 1.25}},
	{pattern: "gpt-4o-*", pricing: ModelPricing{Input: 0.15, Output: 0.60, Cached: 0.075, Reasoning: 0.90, CacheCreation: 0.15}},
	{pattern: "gpt-4o", pricing: ModelPricing{Input: 2.50, Output: 10.00, Cached: 1.25, Reasoning: 15.00, CacheCreation: 2.50}},
	{pattern: "gpt-4*", pricing: ModelPricing{Input: 2.50, Output: 10.00, Cached: 1.25, Reasoning: 15.00, CacheCreation: 2.50}},
	{pattern: "o1-*", pricing: ModelPricing{Input: 3.00, Output: 12.00, Cached: 1.50, Reasoning: 18.00, CacheCreation: 3.00}},
	{pattern: "o1", pricing: ModelPricing{Input: 15.00, Output: 60.00, Cached: 7.50, Reasoning: 90.00, CacheCreation: 15.00}},
	{pattern: "qwen3-coder-*", pricing: ModelPricing{Input: 1.00, Output: 4.00, Cached: 0.50, Reasoning: 6.00, CacheCreation: 1.00}},
	{pattern: "qwen*", pricing: ModelPricing{Input: 0.50, Output: 2.00, Cached: 0.25, Reasoning: 3.00, CacheCreation: 0.50}},
	{pattern: "kimi-*", pricing: ModelPricing{Input: 1.00, Output: 4.00, Cached: 0.50, Reasoning: 6.00, CacheCreation: 1.00}},
	{pattern: "deepseek-*", pricing: ModelPricing{Input: 0.14, Output: 0.28, Cached: 0.0028, Reasoning: 0.28, CacheCreation: 0.14}},
	{pattern: "minimax-*", pricing: ModelPricing{Input: 0.50, Output: 2.00, Cached: 0.25, Reasoning: 3.00, CacheCreation: 0.50}},
	{pattern: "grok-*", pricing: ModelPricing{Input: 0.50, Output: 2.00, Cached: 0.25, Reasoning: 3.00, CacheCreation: 0.50}},
}

func matchGlob(pattern, text string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return strings.EqualFold(pattern, text)
	}

	var sb strings.Builder
	_, _ = sb.WriteString("(?i)^")

	for i, p := range parts {
		if i > 0 {
			_, _ = sb.WriteString(".*")
		}

		_, _ = sb.WriteString(regexp.QuoteMeta(p))
	}

	_, _ = sb.WriteString("$")

	re, err := regexp.Compile(sb.String())
	if err != nil {
		return false
	}

	return re.MatchString(text)
}

// GetPricingForModel resolves pricing for model/provider.
func GetPricingForModel(model string) (ModelPricing, bool) {
	baseModel := model
	if i := strings.LastIndex(model, "/"); i >= 0 {
		baseModel = model[i+1:]
	}

	if p, ok := modelPricingTable[baseModel]; ok {
		return p, true
	}

	if p, ok := modelPricingTable[model]; ok {
		return p, true
	}

	for _, entry := range patternPricingList {
		if matchGlob(entry.pattern, baseModel) || matchGlob(entry.pattern, model) {
			return entry.pricing, true
		}
	}

	return ModelPricing{Input: 1.00, Output: 4.00, Cached: 0.25, Reasoning: 4.00, CacheCreation: 1.00}, false
}

// CalculateCost calculates estimated USD cost from prompt, cached, and completion tokens.
func CalculateCost(provider, model string, promptTokens, cachedTokens, completionTokens int) float64 {
	_ = provider
	p, _ := GetPricingForModel(model)

	nonCachedPrompt := promptTokens - cachedTokens
	if nonCachedPrompt < 0 {
		nonCachedPrompt = 0
	}

	inputRate := p.Input / 1000000.0

	cachedRate := p.Cached / 1000000.0
	if p.Cached == 0 {
		cachedRate = inputRate
	}

	outputRate := p.Output / 1000000.0

	cost := (float64(nonCachedPrompt) * inputRate) +
		(float64(cachedTokens) * cachedRate) +
		(float64(completionTokens) * outputRate)

	return math.Round(cost*1000000) / 1000000
}

package executor

import (
	"net/http"
	"sync"
)

var (
	specialized  = map[string]Executor{}
	defaultCache = map[string]*DefaultExecutor{}
	registryMu   sync.Mutex
	sharedClient *http.Client
)

func init() {
	sharedClient = &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
}

// RegisterSpecialized registers a provider-specific executor.
func RegisterSpecialized(provider string, exec Executor) {
	registryMu.Lock()
	defer registryMu.Unlock()

	specialized[provider] = exec
}

// GetExecutor returns specialized executor or provider-scoped DefaultExecutor.
func GetExecutor(provider string) Executor {
	registryMu.Lock()
	defer registryMu.Unlock()

	if e, ok := specialized[provider]; ok {
		return e
	}
	// aliases
	aliases := map[string]string{
		"cu":   "cursor",
		"gcli": "grok-cli",
		"gb":   "grok-cli",
		"mmf":  "mimo-free",
		"qd":   "qoder",
		"xmtp": "xiaomi-tokenplan",
		"cb":   "codebuddy-cn",
		"zd":   "zed",
		"pplx": "perplexity-web",
		"tr":   "trae",
		"ws":   "windsurf",
		"dv":   "devin-cli",
	}
	if alias, ok := aliases[provider]; ok {
		if e, ok := specialized[alias]; ok {
			return e
		}

		provider = alias
	}

	if e, ok := defaultCache[provider]; ok {
		return e
	}

	e := NewDefaultForProvider(sharedClient, provider)
	defaultCache[provider] = e

	return e
}

// HasSpecializedExecutor reports whether provider has a non-default executor.
func HasSpecializedExecutor(provider string) bool {
	registryMu.Lock()
	defer registryMu.Unlock()

	_, ok := specialized[provider]

	return ok
}

package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func init() {
	RegisterSpecialized("kiro", &KiroExecutor{
		Base: Base{
			Provider: "kiro",
			BaseURLs: []string{
				"https://q.us-east-1.amazonaws.com/generateAssistantResponse",
				"https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse",
			},
		},
	})
}

type KiroExecutor struct {
	Base
}

func (e *KiroExecutor) orderedURLs(cred Credentials) []string {
	base := e.BaseURLs

	if b := strings.TrimRight(cred.BaseURL, "/"); b != "" {
		if !strings.Contains(b, "generateAssistantResponse") {
			b = b + "/generateAssistantResponse"
		}

		return []string{b}
	}

	authMethod := strPSD(cred, "authMethod")
	// api_key / external_idp / idc → amazonaws first
	isCW := authMethod == "api_key" || authMethod == "external_idp" || authMethod == "idc" ||
		(cred.APIKey != "" && cred.AccessToken == "")
	if !isCW {
		// kiro.dev first for social OAuth
		return []string{
			"https://runtime.us-east-1.kiro.dev/generateAssistantResponse",
			"https://q.us-east-1.amazonaws.com/generateAssistantResponse",
			"https://codewhisperer.us-east-1.amazonaws.com/generateAssistantResponse",
		}
	}

	region := strPSD(cred, "region")
	if region == "" {
		region = "us-east-1"
	}

	urls := []string{
		fmt.Sprintf("https://q.%s.amazonaws.com/generateAssistantResponse", region),
		fmt.Sprintf("https://codewhisperer.%s.amazonaws.com/generateAssistantResponse", region),
	}
	if len(base) > 0 {
		urls = append(urls, base...)
	}

	return urls
}

func (e *KiroExecutor) buildHeaders(cred Credentials, stream bool) http.Header {
	h := make(http.Header)
	h.Set("Content-Type", "application/json")
	h.Set("Amz-Sdk-Request", "attempt=1; max=3")
	h.Set("Amz-Sdk-Invocation-Id", randomUUIDSimple())

	authMethod := strPSD(cred, "authMethod")
	if authMethod == "api_key" || (cred.APIKey != "" && cred.AccessToken == "") {
		tok := cred.APIKey
		if tok == "" {
			tok = cred.AccessToken
		}

		h.Set("Authorization", "Bearer "+tok)
		h.Set("tokentype", "API_KEY")
	} else if cred.AccessToken != "" {
		h.Set("Authorization", "Bearer "+cred.AccessToken)

		if authMethod == "external_idp" {
			h.Set("TokenType", "EXTERNAL_IDP")
		}
	} else if cred.APIKey != "" {
		h.Set("Authorization", "Bearer "+cred.APIKey)
	}

	if stream {
		h.Set("Accept", "application/vnd.amazon.eventstream")
	}

	return h
}

func (e *KiroExecutor) Execute(ctx context.Context, cred Credentials, model string, body []byte, stream bool) (*Result, error) {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}

	if _, ok := m["conversationState"]; !ok {
		m = map[string]any{
			"conversationState": map[string]any{
				"chatTriggerType": "MANUAL",
				"currentMessage": map[string]any{
					"userInputMessage": map[string]any{
						"content": fmt.Sprint(m["messages"]),
						"modelId": model,
					},
				},
			},
		}
	}

	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	var lastErr error

	for _, url := range e.orderedURLs(cred) {
		res, err := e.DoPOST(ctx, url, e.buildHeaders(cred, stream), payload)
		if err != nil {
			lastErr = err
			continue
		}
		// retry next host on 429/5xx
		if res.StatusCode == 429 || res.StatusCode >= 500 {
			DrainBody(res.Body)
			lastErr = fmt.Errorf("kiro HTTP %d", res.StatusCode)

			continue
		}

		if res.StatusCode >= 400 {
			return res, nil
		}
		// Transform EventStream → OpenAI SSE (matches 9router KiroExecutor.execute)
		return wrapKiroResult(res, model), nil
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, fmt.Errorf("kiro: no endpoints available")
}

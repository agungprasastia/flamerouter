package qoder

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestConstants(t *testing.T) {
	if QODER_OPENAPI_BASE != "https://openapi.qoder.sh" {
		t.Errorf("unexpected QODER_OPENAPI_BASE: %s", QODER_OPENAPI_BASE)
	}
	if QODER_CHAT_SIG_PATH != "/api/v2/service/pro/sse/agent_chat_generation" {
		t.Errorf("unexpected QODER_CHAT_SIG_PATH: %s", QODER_CHAT_SIG_PATH)
	}
	if len(QODER_MODEL_MAP) == 0 {
		t.Error("QODER_MODEL_MAP is empty")
	}
	if QODER_MODEL_MAP["auto"] != "auto" {
		t.Error("missing auto in QODER_MODEL_MAP")
	}
}

func TestQoderEncodeBody(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "empty",
			input: "",
		},
		{
			name:  "simple string",
			input: "hello world",
		},
		{
			name:  "json payload",
			input: `{"model":"auto","messages":[{"role":"user","content":"hi"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QoderEncodeBody([]byte(tt.input))
			if tt.input == "" {
				if got != "" {
					t.Fatalf("expected empty, got %q", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatal("expected non-empty output")
			}
			// Verify length matches standard base64 length
			stdB64 := base64.StdEncoding.EncodeToString([]byte(tt.input))
			if len(got) != len(stdB64) {
				t.Fatalf("expected length %d, got %d", len(stdB64), len(got))
			}
		})
	}
}

func TestQoderEncodeBodyDeterministicVector(t *testing.T) {
	input := []byte("test")
	got := QoderEncodeBody(input)
	expected := "$$F^J_JH"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestAESEncryptCBCBase64(t *testing.T) {
	key := "1234567890123456" // 16 bytes
	plaintext := []byte(`{"channel":"cosy","client_type":"vscode"}`)

	b64, err := AESEncryptCBCBase64(plaintext, key)
	if err != nil {
		t.Fatalf("AESEncryptCBCBase64 failed: %v", err)
	}

	rawCipher, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		t.Fatalf("aes cipher init failed: %v", err)
	}

	iv := []byte(key)[:16]
	mode := cipher.NewCBCDecrypter(block, iv)
	decrypted := make([]byte, len(rawCipher))
	mode.CryptBlocks(decrypted, rawCipher)

	// Strip PKCS7 padding
	if len(decrypted) == 0 {
		t.Fatal("empty decrypted data")
	}
	padLen := int(decrypted[len(decrypted)-1])
	unpadded := decrypted[:len(decrypted)-padLen]

	if !bytes.Equal(unpadded, plaintext) {
		t.Fatalf("decrypted mismatch: got %s, want %s", string(unpadded), string(plaintext))
	}
}

func TestRSAEncryptPKCS1v15Base64(t *testing.T) {
	data := []byte("16byte-secret-key")
	b64, err := RSAEncryptPKCS1v15Base64(data)
	if err != nil {
		t.Fatalf("RSAEncryptPKCS1v15Base64 failed: %v", err)
	}

	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("base64 decode failed: %v", err)
	}

	// 1024-bit RSA key produces 128 bytes ciphertext
	if len(decoded) != 128 {
		t.Fatalf("expected 128 bytes RSA ciphertext, got %d", len(decoded))
	}
}

func TestComputeSigPath(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{
			url:  "https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result",
			want: "/api/v2/service/pro/sse/agent_chat_generation",
		},
		{
			url:  "https://api3.qoder.sh/algo/api/v2/model/list",
			want: "/api/v2/model/list",
		},
		{
			url:  "https://api3.qoder.sh/direct/path",
			want: "/direct/path",
		},
		{
			url:  "invalid-url-%%%",
			want: "",
		},
	}

	for _, tt := range tests {
		got := ComputeSigPath(tt.url)
		if got != tt.want {
			t.Errorf("ComputeSigPath(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestBuildCosyHeaders(t *testing.T) {
	body := []byte(`{"prompt":"hello"}`)
	requestURL := "https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common"
	creds := CosyCreds{
		UserID:    "user_12345",
		AuthToken: "dt-token-abcde",
		Name:      "Test User",
		Email:     "test@example.com",
		MachineID: "fixed-machine-id",
	}

	headers, err := BuildCosyHeaders(body, requestURL, creds)
	if err != nil {
		t.Fatalf("BuildCosyHeaders failed: %v", err)
	}

	requiredHeaders := []string{
		"Authorization",
		"Cosy-Key",
		"Cosy-User",
		"Cosy-Date",
		"Cosy-Version",
		"Cosy-Machineid",
		"Cosy-Machinetoken",
		"Cosy-Machinetype",
		"Cosy-Machineos",
		"Cosy-Clienttype",
		"Cosy-Clientip",
		"Cosy-Bodyhash",
		"Cosy-Bodylength",
		"Cosy-Sigpath",
		"Cosy-Data-Policy",
		"Cosy-Organization-Id",
		"Cosy-Organization-Tags",
		"Login-Version",
		"X-Request-Id",
	}

	for _, h := range requiredHeaders {
		val, ok := headers[h]
		if !ok {
			t.Errorf("missing expected header %q", h)
		}
		if val == "" && h != "Cosy-Organization-Id" && h != "Cosy-Organization-Tags" {
			t.Errorf("header %q should not be empty", h)
		}
	}

	if headers["Cosy-User"] != "user_12345" {
		t.Errorf("Cosy-User mismatch: got %q", headers["Cosy-User"])
	}
	if headers["Cosy-Machineid"] != "fixed-machine-id" {
		t.Errorf("Cosy-Machineid mismatch: got %q", headers["Cosy-Machineid"])
	}
	if headers["Cosy-Sigpath"] != "/api/v2/service/pro/sse/agent_chat_generation" {
		t.Errorf("Cosy-Sigpath mismatch: got %q", headers["Cosy-Sigpath"])
	}
	if headers["Cosy-Bodylength"] != "18" {
		t.Errorf("Cosy-Bodylength mismatch: got %q", headers["Cosy-Bodylength"])
	}

	// Verify Authorization format
	auth := headers["Authorization"]
	if !strings.HasPrefix(auth, "Bearer COSY.") {
		t.Fatalf("invalid Authorization header prefix: %q", auth)
	}
	parts := strings.Split(strings.TrimPrefix(auth, "Bearer COSY."), ".")
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts after Bearer COSY., got %d", len(parts))
	}

	payloadB64 := parts[0]
	sig := parts[1]

	// Verify payload can be base64 decoded and unmarshalled
	payloadBytes, err := base64.StdEncoding.DecodeString(payloadB64)
	if err != nil {
		t.Fatalf("failed to decode payloadB64: %v", err)
	}
	if !strings.Contains(string(payloadBytes), `"version":"v1"`) {
		t.Errorf("payload missing version: %s", string(payloadBytes))
	}

	// Verify signature MD5 hash calculation
	cosyKey := headers["Cosy-Key"]
	timestamp := headers["Cosy-Date"]
	sigPath := headers["Cosy-Sigpath"]

	sigInput := payloadB64 + "\n" + cosyKey + "\n" + timestamp + "\n" + string(body) + "\n" + sigPath
	expectedSigMD5 := md5.Sum([]byte(sigInput))
	expectedSig := hex.EncodeToString(expectedSigMD5[:])

	if sig != expectedSig {
		t.Errorf("signature mismatch: got %s, want %s", sig, expectedSig)
	}
}

func TestBuildCosyHeadersValidation(t *testing.T) {
	_, err := BuildCosyHeaders([]byte(""), "http://localhost", CosyCreds{})
	if err == nil {
		t.Error("expected error for empty UserID")
	}

	_, err = BuildCosyHeaders([]byte(""), "http://localhost", CosyCreds{UserID: "u1"})
	if err == nil {
		t.Error("expected error for empty AuthToken")
	}
}

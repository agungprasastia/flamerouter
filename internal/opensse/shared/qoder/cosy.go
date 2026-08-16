package qoder

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CosyCreds holds credentials required to build COSY headers.
type CosyCreds struct {
	UserID    string
	AuthToken string
	Name      string
	Email     string
	MachineID string
}

// GenerateMachineID returns a fresh UUID string for machine identification.
func GenerateMachineID() string {
	return uuid.New().String()
}

// generateAESKey returns a 16-character string derived from the first 16 chars of a new UUID.
func generateAESKey() string {
	u := uuid.New().String()
	if len(u) > 16 {
		return u[:16]
	}
	return u
}

// pkcs7Pad appends PKCS7 padding to data for the given block size.
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	padText := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padText...)
}

// AESEncryptCBCBase64 encrypts plaintext with AES-128-CBC using keyStr as key and IV, returning base64 ciphertext.
func AESEncryptCBCBase64(plaintext []byte, keyStr string) (string, error) {
	keyBytes := []byte(keyStr)
	if len(keyBytes) != 16 {
		return "", fmt.Errorf("aes key must be 16 bytes, got %d", len(keyBytes))
	}

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}

	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))

	iv := keyBytes[:16]
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, padded)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// RSAEncryptPKCS1v15Base64 encrypts data using RSA PKCS1v15 with QODER_RSA_PUBLIC_KEY, returning base64.
func RSAEncryptPKCS1v15Base64(data []byte) (string, error) {
	block, _ := pem.Decode([]byte(QODER_RSA_PUBLIC_KEY))
	if block == nil {
		return "", errors.New("failed to parse RSA public key PEM")
	}

	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse public key: %w", err)
	}

	pubKey, ok := pubInterface.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("not an RSA public key")
	}

	encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, pubKey, data)
	if err != nil {
		return "", fmt.Errorf("rsa encrypt: %w", err)
	}

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

type userInfoPayload struct {
	UID                string `json:"uid"`
	SecurityOAuthToken string `json:"security_oauth_token"`
	Name               string `json:"name"`
	AID                string `json:"aid"`
	Email              string `json:"email"`
}

type encryptedUserInfo struct {
	CosyKey string
	Info    string
}

func encryptUserInfo(creds CosyCreds) (*encryptedUserInfo, error) {
	aesKey := generateAESKey()
	userPayload := userInfoPayload{
		UID:                creds.UserID,
		SecurityOAuthToken: creds.AuthToken,
		Name:               creds.Name,
		AID:                "",
		Email:              creds.Email,
	}

	plaintext, err := json.Marshal(userPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal user info: %w", err)
	}

	infoB64, err := AESEncryptCBCBase64(plaintext, aesKey)
	if err != nil {
		return nil, fmt.Errorf("aes encrypt user info: %w", err)
	}

	cosyKeyB64, err := RSAEncryptPKCS1v15Base64([]byte(aesKey))
	if err != nil {
		return nil, fmt.Errorf("rsa encrypt aes key: %w", err)
	}

	return &encryptedUserInfo{
		CosyKey: cosyKeyB64,
		Info:    infoB64,
	}, nil
}

func md5Hex(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}

// ComputeSigPath strips leading "/algo" prefix from the request URL pathname.
func ComputeSigPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	pathname := u.Path
	if strings.HasPrefix(pathname, "/algo") {
		return pathname[len("/algo"):]
	}
	return pathname
}

type outerPayload struct {
	Version     string `json:"version"`
	RequestID   string `json:"requestId"`
	Info        string `json:"info"`
	CosyVersion string `json:"cosyVersion"`
	IDEVersion  string `json:"ideVersion"`
}

// BuildCosyHeaders generates the 17 Cosy-* and standard headers required for Qoder inference.
func BuildCosyHeaders(body []byte, requestURL string, creds CosyCreds) (map[string]string, error) {
	if creds.UserID == "" {
		return nil, errors.New("cosy: user id is empty")
	}
	if creds.AuthToken == "" {
		return nil, errors.New("cosy: auth token is empty")
	}

	encInfo, err := encryptUserInfo(creds)
	if err != nil {
		return nil, err
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	reqUUID := uuid.New().String()

	payloadObj := outerPayload{
		Version:     "v1",
		RequestID:   reqUUID,
		Info:        encInfo.Info,
		CosyVersion: QODER_IDE_VERSION,
		IDEVersion:  "",
	}
	payloadBytes, err := json.Marshal(payloadObj)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	payloadB64 := base64.StdEncoding.EncodeToString(payloadBytes)

	sigPath := ComputeSigPath(requestURL)
	// Signature input: payloadB64 + "\n" + cosyKey + "\n" + timestamp + "\n" + body + "\n" + sigPath
	var sigBuf bytes.Buffer
	sigBuf.WriteString(payloadB64)
	sigBuf.WriteByte('\n')
	sigBuf.WriteString(encInfo.CosyKey)
	sigBuf.WriteByte('\n')
	sigBuf.WriteString(timestamp)
	sigBuf.WriteByte('\n')
	sigBuf.Write(body)
	sigBuf.WriteByte('\n')
	sigBuf.WriteString(sigPath)

	sig := md5Hex(sigBuf.Bytes())

	machineID := creds.MachineID
	if machineID == "" {
		machineID = GenerateMachineID()
	}

	bodyHash := md5Hex(body)
	bodyLength := strconv.Itoa(len(body))

	headers := map[string]string{
		"Authorization":          "Bearer COSY." + payloadB64 + "." + sig,
		"Cosy-Key":              encInfo.CosyKey,
		"Cosy-User":             creds.UserID,
		"Cosy-Date":             timestamp,
		"Cosy-Version":          QODER_IDE_VERSION,
		"Cosy-Machineid":        machineID,
		"Cosy-Machinetoken":     machineID,
		"Cosy-Machinetype":      QODER_MACHINE_TYPE,
		"Cosy-Machineos":        QODER_MACHINE_OS,
		"Cosy-Clienttype":       QODER_CLIENT_TYPE,
		"Cosy-Clientip":         "127.0.0.1",
		"Cosy-Bodyhash":         bodyHash,
		"Cosy-Bodylength":       bodyLength,
		"Cosy-Sigpath":          sigPath,
		"Cosy-Data-Policy":      QODER_DATA_POLICY,
		"Cosy-Organization-Id":   "",
		"Cosy-Organization-Tags": "",
		"Login-Version":         QODER_LOGIN_VERSION,
		"X-Request-Id":          uuid.New().String(),
	}

	return headers, nil
}

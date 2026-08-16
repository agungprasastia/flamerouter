package qoder

import (
	"encoding/base64"
)

const (
	qoderStdAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	qoderCustomAlphabet = "_doRTgHZBKcGVjlvpC,@aFSx#DPuNJme&i*MzLOEn)sUrthbf%Y^w.(kIQyXqWA!"
)

var qoderS2C [128]byte

func init() {
	for i := range qoderS2C {
		qoderS2C[i] = byte(i)
	}

	for i := 0; i < len(qoderStdAlphabet) && i < len(qoderCustomAlphabet); i++ {
		qoderS2C[qoderStdAlphabet[i]] = qoderCustomAlphabet[i]
	}

	qoderS2C['='] = '$'
}

// QoderEncodeBody encodes raw body bytes using Qoder's custom base64-rearranged substitution.
// 1. Base64 encode standard
// 2. Split into 3 parts at a = len/3: [tail: from len-a to end][mid: from a to len-a][head: from 0 to a]
// 3. Map characters via custom alphabet table (replacing '=' with '$').
func QoderEncodeBody(rawBody []byte) string {
	std := base64.StdEncoding.EncodeToString(rawBody)

	n := len(std)
	if n == 0 {
		return ""
	}

	a := n / 3
	// rearranged: std[n-a:] + std[a:n-a] + std[:a]
	rearranged := make([]byte, n)
	tailLen := copy(rearranged, std[n-a:])
	midLen := copy(rearranged[tailLen:], std[a:n-a])
	copy(rearranged[tailLen+midLen:], std[:a])

	out := make([]byte, n)

	for i := 0; i < n; i++ {
		c := rearranged[i]
		if c < 128 {
			out[i] = qoderS2C[c]
		} else {
			out[i] = c
		}
	}

	return string(out)
}

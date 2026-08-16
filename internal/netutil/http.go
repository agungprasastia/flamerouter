// Package netutil provides common network and HTTP utilities.
package netutil

import (
	"errors"
	"net/http"
)

// DoHTTP wraps client.Do (or http.DefaultClient if client is nil) and guarantees
// that if err == nil, both resp and resp.Body are non-nil.
func DoHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.New("http: nil response returned")
	}

	if resp.Body == nil {
		return nil, errors.New("http: nil response body returned")
	}

	return resp, nil
}

package usage

import (
	"fmt"
	"net/http"
)

// doHTTP executes an HTTP request and ensures safe response checking for nilaway.
func doHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if res == nil || res.Body == nil {
		return nil, fmt.Errorf("empty response received")
	}
	return res, nil
}

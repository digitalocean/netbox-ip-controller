/*
Copyright 2022 DigitalOcean

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at:

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package netbox

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseAndValidateURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		shouldError bool
	}{{
		name:        "invalid",
		url:         "*?!",
		shouldError: true,
	}, {
		name:        "without scheme",
		url:         "example.com/api",
		shouldError: true,
	}, {
		name:        "without hostname",
		url:         "http:///api",
		shouldError: true,
	}, {
		name:        "valid",
		url:         "http://example.com:1234/api",
		shouldError: false,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseAndValidateURL(test.url)
			if err != nil && !test.shouldError {
				t.Errorf("want no error, got: %q\n", err)
			} else if err == nil && test.shouldError {
				t.Error("want an error, got nil")
			}
		})
	}
}

func TestRetryableHTTPClient(t *testing.T) {
	client := retryableHTTPClient(1)

	t.Run("idempotent requests retried", func(t *testing.T) {
		var numCalls int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			numCalls++
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		client.Get(ts.URL)

		numRetries := numCalls - 1
		if numRetries != 1 {
			t.Errorf("want %d retries, got %d", 1, numRetries)
		}
	})

	t.Run("non-idempotent requests not retried", func(t *testing.T) {
		var numCalls int
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			numCalls++
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		client.Post(ts.URL, "application/json", bytes.NewBufferString(`{}`))

		numRetries := numCalls - 1
		if numRetries != 0 {
			t.Errorf("want %d retries, got %d", 0, numRetries)
		}
	})
}

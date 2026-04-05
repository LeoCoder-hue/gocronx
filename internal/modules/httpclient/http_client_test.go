package httpclient

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mockDoer func(req *http.Request) (*http.Response, error)

func (m mockDoer) Do(req *http.Request) (*http.Response, error) {
	return m(req)
}

func withMockClient(t *testing.T, doer mockDoer) {
	t.Helper()
	original := clientFactory
	clientFactory = func(timeout int) httpDoer {
		return doer
	}
	t.Cleanup(func() { clientFactory = original })
}

func TestGetRequest(t *testing.T) {
	withMockClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("expected GET, got %s", req.Method)
		}
		if ua := req.Header.Get("User-Agent"); ua != "golang/gocron" {
			t.Fatalf("unexpected user-agent %s", ua)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     http.Header{},
		}, nil
	})

	resp := Get("http://example.com", 0)
	if resp.StatusCode != 200 || resp.Body != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestPostParamsRequest(t *testing.T) {
	withMockClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", req.Method)
		}
		if req.Header.Get("Content-type") != "application/x-www-form-urlencoded" {
			t.Fatalf("unexpected content-type %s", req.Header.Get("Content-type"))
		}
		body, _ := io.ReadAll(req.Body)
		if string(body) != "a=1&b=2" {
			t.Fatalf("unexpected body %s", string(body))
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("echo:" + string(body))),
			Header:     http.Header{},
		}, nil
	})

	resp := PostParams("http://example.com", "a=1&b=2", 0)
	if resp.StatusCode != 200 || resp.Body != "echo:a=1&b=2" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestPostJsonRequest(t *testing.T) {
	withMockClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Content-type") != "application/json" {
			t.Fatalf("unexpected content-type %s", req.Header.Get("Content-type"))
		}
		body, _ := io.ReadAll(req.Body)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("json:" + string(body))),
			Header:     http.Header{},
		}, nil
	})

	resp := PostJson("http://example.com", `{"name":"gocron"}`, 0)
	if resp.StatusCode != 200 || resp.Body != `json:{"name":"gocron"}` {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRequestHandlesClientError(t *testing.T) {
	withMockClient(t, func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("timeout")
	})
	resp := Get("http://example.com", 1)
	if resp.StatusCode != 0 || !strings.Contains(resp.Body, "执行HTTP请求错误-timeout") {
		t.Fatalf("expected client error message, got %+v", resp)
	}
}

func TestRequestHandlesReadError(t *testing.T) {
	withMockClient(t, func(req *http.Request) (*http.Response, error) {
		rc := io.NopCloser(io.Reader(&failingReader{}))
		return &http.Response{StatusCode: 200, Body: rc, Header: http.Header{}}, nil
	})
	resp := Get("http://example.com", 0)
	if resp.StatusCode != 0 || !strings.Contains(resp.Body, "读取HTTP请求返回值失败") {
		t.Fatalf("expected read error message, got %+v", resp)
	}
}

type failingReader struct{}

func (f *failingReader) Read(p []byte) (int, error) {
	return 0, errors.New("boom")
}

func TestCreateRequestError(t *testing.T) {
	resp := createRequestError(fmt.Errorf("boom"))
	if resp.StatusCode != 0 || !strings.Contains(resp.Body, "boom") {
		t.Fatalf("unexpected error wrapper: %+v", resp)
	}
}

func TestSetCustomHeaders(t *testing.T) {
	t.Run("valid JSON headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		SetCustomHeaders(req, `{"Authorization":"Bearer token123","X-Custom":"value"}`)
		if req.Header.Get("Authorization") != "Bearer token123" {
			t.Fatalf("expected Authorization header, got %q", req.Header.Get("Authorization"))
		}
		if req.Header.Get("X-Custom") != "value" {
			t.Fatalf("expected X-Custom header, got %q", req.Header.Get("X-Custom"))
		}
	})

	t.Run("empty string is no-op", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		SetCustomHeaders(req, "")
		if len(req.Header) != 0 {
			t.Fatalf("expected no headers for empty input, got %v", req.Header)
		}
	})

	t.Run("invalid JSON is no-op", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		SetCustomHeaders(req, "not json")
		// Should not panic, just skip
	})

	t.Run("blocked headers are skipped", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://example.com", nil)
		SetCustomHeaders(req, `{"Host":"evil.com","X-Safe":"ok","Transfer-Encoding":"chunked"}`)
		if req.Header.Get("Host") != "" {
			t.Fatal("Host header should be blocked")
		}
		if req.Header.Get("Transfer-Encoding") != "" {
			t.Fatal("Transfer-Encoding header should be blocked")
		}
		if req.Header.Get("X-Safe") != "ok" {
			t.Fatalf("safe header should be set, got %q", req.Header.Get("X-Safe"))
		}
	})
}

func TestIsBlockedHeader(t *testing.T) {
	blocked := []string{"Host", "host", "HOST", "Transfer-Encoding", "connection", "Content-Length", "Upgrade"}
	for _, h := range blocked {
		if !IsBlockedHeader(h) {
			t.Errorf("expected %q to be blocked", h)
		}
	}

	allowed := []string{"Authorization", "X-Custom", "Content-Type", "Accept"}
	for _, h := range allowed {
		if IsBlockedHeader(h) {
			t.Errorf("expected %q to be allowed", h)
		}
	}
}

func TestValidateHeaders(t *testing.T) {
	t.Run("empty is ok", func(t *testing.T) {
		if err := ValidateHeaders(""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid headers", func(t *testing.T) {
		if err := ValidateHeaders(`{"Authorization":"Bearer x"}`); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		err := ValidateHeaders("not json")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("blocked header rejected", func(t *testing.T) {
		err := ValidateHeaders(`{"Host":"evil.com"}`)
		if err == nil {
			t.Fatal("expected error for blocked header")
		}
		if !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("mixed blocked and safe", func(t *testing.T) {
		err := ValidateHeaders(`{"Authorization":"ok","Transfer-Encoding":"chunked"}`)
		if err == nil {
			t.Fatal("expected error for blocked header")
		}
	})
}

func TestGetWithHeaders(t *testing.T) {
	withMockClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer abc" {
			t.Fatalf("expected Authorization header, got %q", req.Header.Get("Authorization"))
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     http.Header{},
		}, nil
	})
	resp := GetWithHeaders("http://example.com", `{"Authorization":"Bearer abc"}`, 10)
	if resp.StatusCode != 200 || resp.Body != "ok" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestPostJsonWithHeaders(t *testing.T) {
	withMockClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Api-Key") != "secret" {
			t.Fatalf("expected X-Api-Key header, got %q", req.Header.Get("X-Api-Key"))
		}
		ct := req.Header.Get("Content-type")
		if !strings.Contains(ct, "application/json") {
			t.Fatalf("expected JSON content-type, got %q", ct)
		}
		body, _ := io.ReadAll(req.Body)
		if string(body) != `{"k":"v"}` {
			t.Fatalf("unexpected body: %s", string(body))
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("ok")),
			Header:     http.Header{},
		}, nil
	})
	resp := PostJsonWithHeaders("http://example.com", `{"k":"v"}`, `{"X-Api-Key":"secret"}`, 10)
	if resp.StatusCode != 200 {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}
}

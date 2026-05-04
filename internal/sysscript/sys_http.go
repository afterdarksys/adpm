package sysscript

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.starlark.net/starlark"
)

// =============================================================================
// sys.net extended — http_post, http_request
//
//   sys.net.http_post(url, body, content_type="application/json", headers={})
//     → {status_code, body, error}
//
//   sys.net.http_request(method, url, body="", headers={}, timeout_ms=10000)
//     → {status_code, body, error}
// =============================================================================

func (engine *Engine) netHTTPPost(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var reqURL, body, contentType string
	var headersVal starlark.Value
	if err := starlark.UnpackArgs("http_post", args, kwargs,
		"url", &reqURL,
		"body", &body,
		"content_type?", &contentType,
		"headers?", &headersVal,
	); err != nil {
		return starlark.None, err
	}
	if contentType == "" {
		contentType = "application/json"
	}

	if err := engine.checkNetAllowed(reqURL); err != nil {
		return httpErrResult(err.Error()), nil
	}

	extraHeaders := parseStarlarkHeaders(headersVal)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(body))
	if err != nil {
		return httpErrResult(err.Error()), nil
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return httpErrResult(err.Error()), nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return httpResult(resp.StatusCode, string(respBody), ""), nil
}

func (engine *Engine) netHTTPRequest(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var method, reqURL, body string
	var headersVal starlark.Value
	var timeoutMs int
	if err := starlark.UnpackArgs("http_request", args, kwargs,
		"method", &method,
		"url", &reqURL,
		"body?", &body,
		"headers?", &headersVal,
		"timeout_ms?", &timeoutMs,
	); err != nil {
		return starlark.None, err
	}
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}

	if err := engine.checkNetAllowed(reqURL); err != nil {
		return httpErrResult(err.Error()), nil
	}

	extraHeaders := parseStarlarkHeaders(headersVal)

	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(strings.ToUpper(method), reqURL, bodyReader)
	if err != nil {
		return httpErrResult(err.Error()), nil
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return httpErrResult(err.Error()), nil
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return httpResult(resp.StatusCode, string(respBody), ""), nil
}

// --- helpers ---

func (engine *Engine) checkNetAllowed(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url: %v", err)
	}
	for _, domain := range engine.Entitlements.AllowNetOutbound {
		if domain == "*" || domain == u.Hostname() {
			return nil
		}
	}
	return fmt.Errorf("security exception: outbound to %s not granted", u.Hostname())
}

func parseStarlarkHeaders(val starlark.Value) map[string]string {
	out := map[string]string{}
	if val == nil {
		return out
	}
	d, ok := val.(*starlark.Dict)
	if !ok {
		return out
	}
	for _, kv := range d.Items() {
		k, ok1 := kv[0].(starlark.String)
		v, ok2 := kv[1].(starlark.String)
		if ok1 && ok2 {
			out[string(k)] = string(v)
		}
	}
	return out
}

func httpResult(statusCode int, body, errMsg string) starlark.Value {
	d := starlark.NewDict(3)
	d.SetKey(starlark.String("status_code"), starlark.MakeInt(statusCode))
	d.SetKey(starlark.String("body"), starlark.String(body))
	d.SetKey(starlark.String("error"), starlark.String(errMsg))
	return d
}

func httpErrResult(msg string) starlark.Value {
	return httpResult(0, "", msg)
}

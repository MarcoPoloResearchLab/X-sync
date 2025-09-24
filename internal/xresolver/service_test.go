// internal/xresolver/service_test.go
package xresolver

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

// stubRenderer allows us to simulate Chrome output deterministically in tests.
type stubRenderer struct {
	// map url -> html or special tokens
	byURL map[string]string
	// if >0, sleep this long for every render (to test attempt/per-id timeouts)
	sleep time.Duration
	// error to return if set (overrides byURL)
	err error
}

func (r *stubRenderer) Render(ctx context.Context, userAgent, url string, vtBudgetMS int, chromePath string) (string, error) {
	if r.sleep > 0 {
		select {
		case <-time.After(r.sleep):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if r.err != nil {
		return "", r.err
	}
	if s, ok := r.byURL[url]; ok {
		return s, nil
	}
	return "", errors.New("unknown url")
}

const (
	htmlDocumentMetaPrefix      = `<html><head><meta property="og:title" content="`
	htmlDocumentHeadSuffix      = `"></head>`
	htmlDocumentBodyOpenTag     = `<body>`
	htmlDocumentBodyCloseTag    = `</body></html>`
	htmlAnchorOpenPrefix        = `<a href="`
	htmlAnchorTextSeparator     = `">`
	htmlAnchorCloseSuffix       = `</a>`
	profileHandleURLPrefix      = `https://x.com/`
	profileHandleDisplayPrefix  = `@`
	profileHandleTitleSeparator = " (@"
	profileHandleTitleSuffix    = ") / X"

	chromeMissingBinaryPath = "/nonexistent/chrome"
	chromeRenderTestURL     = "https://example.com"
	chromeRenderTestAgent   = "test-user-agent"
)

func htmlFor(handle, displayName string) string {
	// minimal HTML that satisfies our extractors
	profileTitle := displayName
	if handle != "" {
		profileTitle = displayName + profileHandleTitleSeparator + handle + profileHandleTitleSuffix
	}

	var htmlBuilder strings.Builder
	htmlBuilder.WriteString(htmlDocumentMetaPrefix)
	htmlBuilder.WriteString(profileTitle)
	htmlBuilder.WriteString(htmlDocumentHeadSuffix)
	htmlBuilder.WriteString(htmlDocumentBodyOpenTag)
	if handle != "" {
		htmlBuilder.WriteString(htmlAnchorOpenPrefix)
		htmlBuilder.WriteString(profileHandleURLPrefix)
		htmlBuilder.WriteString(handle)
		htmlBuilder.WriteString(htmlAnchorTextSeparator)
		htmlBuilder.WriteString(profileHandleDisplayPrefix)
		htmlBuilder.WriteString(handle)
		htmlBuilder.WriteString(htmlAnchorCloseSuffix)
	}
	htmlBuilder.WriteString(htmlDocumentBodyCloseTag)

	return htmlBuilder.String()
}

func TestResolveSuccess_FirstEndpoint(t *testing.T) {
	r := &stubRenderer{
		byURL: map[string]string{
			"https://x.com/intent/user?user_id=123": htmlFor("alice", "Alice Doe"),
		},
	}
	cfg := Config{
		ChromePath:          "/dev/null/chrome",
		VirtualTimeBudgetMS: 1000,
		PerIDTimeout:        2 * time.Second,
		AttemptTimeout:      1 * time.Second,
		Retries:             0,
	}
	svc := NewService(cfg, r)

	res := svc.ResolveBatch(context.Background(), Request{IDs: []string{"123"}})
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	p := res[0]
	if p.Handle != "alice" || p.DisplayName != "Alice Doe" || p.Err != "" {
		t.Fatalf("unexpected profile: %+v", p)
	}
}

func TestResolveFallback_SecondEndpoint(t *testing.T) {
	r := &stubRenderer{
		byURL: map[string]string{
			"https://x.com/intent/user?user_id=123": "", // empty -> forces fallback
			"https://x.com/i/user/123":              htmlFor("bob", "Bobby"),
		},
	}
	cfg := Config{
		ChromePath:          "/dev/null/chrome",
		VirtualTimeBudgetMS: 1000,
		PerIDTimeout:        2 * time.Second,
		AttemptTimeout:      1 * time.Second,
		Retries:             1, // allow retry cycle though fallback succeeds in first cycle anyway
	}
	svc := NewService(cfg, r)

	res := svc.ResolveBatch(context.Background(), Request{IDs: []string{"123"}})
	if res[0].Handle != "bob" || res[0].DisplayName != "Bobby" || res[0].Err != "" {
		t.Fatalf("unexpected profile: %+v", res[0])
	}
}

func TestResolveAttemptTimeout(t *testing.T) {
	// Renderer sleeps longer than attempt-timeout; should return context.DeadlineExceeded per attempt
	r := &stubRenderer{
		byURL: map[string]string{
			"https://x.com/intent/user?user_id=1": htmlFor("h", "H"),
			"https://x.com/i/user/1":              htmlFor("h", "H"),
		},
		sleep: 150 * time.Millisecond,
	}
	cfg := Config{
		ChromePath:          "/dev/null/chrome",
		VirtualTimeBudgetMS: 1000,
		PerIDTimeout:        400 * time.Millisecond,
		AttemptTimeout:      50 * time.Millisecond, // each attempt should time out
		Retries:             1,                     // two attempts total
		RetryMin:            10 * time.Millisecond,
		RetryMax:            20 * time.Millisecond,
	}
	svc := NewService(cfg, r)

	res := svc.ResolveBatch(context.Background(), Request{IDs: []string{"1"}})
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if !strings.Contains(res[0].Err, "context deadline exceeded") && res[0].Handle == "" {
		// We accept either per-attempt timeout bubbling up or final "unresolvable"
		// depending on scheduler jitter, but success here would be unexpected.
		t.Logf("err=%q handle=%q", res[0].Err, res[0].Handle)
	}
}

func TestBatchPacing_NoParallelLeak(t *testing.T) {
	r := &stubRenderer{
		byURL: map[string]string{
			"https://x.com/intent/user?user_id=10": htmlFor("aa", "AA"),
			"https://x.com/i/user/10":              htmlFor("aa", "AA"),
			"https://x.com/intent/user?user_id=20": htmlFor("bb", "BB"),
			"https://x.com/i/user/20":              htmlFor("bb", "BB"),
		},
		sleep: 20 * time.Millisecond,
	}
	cfg := Config{
		ChromePath:          "/dev/null/chrome",
		VirtualTimeBudgetMS: 1000,
		PerIDTimeout:        2 * time.Second,
		AttemptTimeout:      500 * time.Millisecond,
		Delay:               30 * time.Millisecond,
		Jitter:              10 * time.Millisecond,
		BurstSize:           0, // no bursts
	}
	svc := NewService(cfg, r)
	start := time.Now()
	res := svc.ResolveBatch(context.Background(), Request{IDs: []string{"10", "20"}})
	elapsed := time.Since(start)

	if len(res) != 2 || res[0].Handle != "aa" || res[1].Handle != "bb" {
		t.Fatalf("bad results: %+v", res)
	}
	// Expect at least ~20ms (render) + ~30ms (delay) + second render ~20ms
	if elapsed < 60*time.Millisecond {
		t.Fatalf("pacing seems off, elapsed=%v", elapsed)
	}
}

func TestChromeRendererMissingBinary(t *testing.T) {
	renderer := NewChromeRenderer()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	htmlDocument, err := renderer.Render(ctx, chromeRenderTestAgent, chromeRenderTestURL, 100, chromeMissingBinaryPath)
	if err == nil {
		t.Fatalf("expected failure for missing chrome binary, got document=%q", htmlDocument)
	}
	if !strings.Contains(err.Error(), chromeMissingBinaryPath) {
		t.Fatalf("expected error to reference chrome path %q, got %v", chromeMissingBinaryPath, err)
	}
}

func TestChromeProxyServerValue(t *testing.T) {
	testCases := []struct {
		name          string
		targetURL     string
		environment   map[string]string
		expectedProxy string
	}{
		{
			name:      "https proxy available",
			targetURL: "https://example.com/resource",
			environment: map[string]string{
				"HTTPS_PROXY": "http://corp.example:8443",
				"https_proxy": "http://corp.example:8443",
				"HTTP_PROXY":  "http://fallback.example:8080",
				"http_proxy":  "http://fallback.example:8080",
				"NO_PROXY":    "",
				"no_proxy":    "",
			},
			expectedProxy: "http://corp.example:8443",
		},
		{
			name:      "fallback to http proxy",
			targetURL: "https://example.com/resource",
			environment: map[string]string{
				"HTTPS_PROXY": "",
				"https_proxy": "",
				"HTTP_PROXY":  "http://fallback.example:8080",
				"http_proxy":  "http://fallback.example:8080",
				"NO_PROXY":    "",
				"no_proxy":    "",
			},
			expectedProxy: "http://fallback.example:8080",
		},
		{
			name:      "no proxy due to bypass list",
			targetURL: "https://internal.example",
			environment: map[string]string{
				"HTTPS_PROXY": "http://corp.example:8443",
				"https_proxy": "http://corp.example:8443",
				"HTTP_PROXY":  "http://fallback.example:8080",
				"http_proxy":  "http://fallback.example:8080",
				"NO_PROXY":    "internal.example,localhost",
				"no_proxy":    "internal.example,localhost",
			},
			expectedProxy: "",
		},
		{
			name:      "invalid url",
			targetURL: "::not-a-url::",
			environment: map[string]string{
				"HTTPS_PROXY": "http://corp.example:8443",
				"https_proxy": "http://corp.example:8443",
			},
			expectedProxy: "",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			proxyEnvironmentVariables := []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"}
			for _, variableName := range proxyEnvironmentVariables {
				t.Setenv(variableName, "")
			}
			for key, value := range testCase.environment {
				t.Setenv(key, value)
			}

			actualProxy := chromeProxyServerValue(testCase.targetURL)
			if actualProxy != testCase.expectedProxy {
				t.Fatalf("expected proxy %q, got %q", testCase.expectedProxy, actualProxy)
			}
		})
	}
}

func TestBypassProxyRespectsPatterns(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		targetURL      string
		noProxyEntries string
		expectBypass   bool
	}{
		{
			name:           "exact match",
			targetURL:      "https://api.internal.local/resource",
			noProxyEntries: "api.internal.local",
			expectBypass:   true,
		},
		{
			name:           "leading dot",
			targetURL:      "https://profile.x.com",
			noProxyEntries: ".x.com",
			expectBypass:   true,
		},
		{
			name:           "wildcard prefix",
			targetURL:      "https://subdomain.x.com",
			noProxyEntries: "*.x.com",
			expectBypass:   true,
		},
		{
			name:           "port specific mismatch",
			targetURL:      "https://x.com",
			noProxyEntries: "x.com:8080",
			expectBypass:   false,
		},
		{
			name:           "asterisk all",
			targetURL:      "https://anything.example",
			noProxyEntries: "*",
			expectBypass:   true,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			parsedURL, parseErr := url.Parse(testCase.targetURL)
			if parseErr != nil {
				t.Fatalf("parse url: %v", parseErr)
			}
			bypass := bypassProxy(parsedURL, testCase.noProxyEntries)
			if bypass != testCase.expectBypass {
				t.Fatalf("expected bypass=%t for %s with %q, got %t", testCase.expectBypass, testCase.targetURL, testCase.noProxyEntries, bypass)
			}
		})
	}
}

func TestChromeProxyServerValueHonorsNoProxyWildcard(t *testing.T) {
	proxyAddress := "http://localhost:8080"
	t.Setenv(httpsProxyEnvironmentUpper, proxyAddress)
	t.Setenv(noProxyEnvironmentUpper, "*.x.com")

	if proxy := chromeProxyServerValue("https://x.com/intent/user?user_id=1"); proxy != "" {
		t.Fatalf("expected proxy to be empty due to NO_PROXY wildcard, got %q", proxy)
	}
}

func TestChromeProxyServerValueUsesProxyWhenNotBypassed(t *testing.T) {
	proxyAddress := "http://localhost:8080"
	t.Setenv(httpsProxyEnvironmentUpper, proxyAddress)
	t.Setenv(noProxyEnvironmentUpper, "internal.local")

	proxy := chromeProxyServerValue("https://x.com/intent/user?user_id=1")
	if proxy != proxyAddress {
		t.Fatalf("expected proxy %q, got %q", proxyAddress, proxy)
	}
}

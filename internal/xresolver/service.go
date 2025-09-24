// internal/xresolver/service.go
package xresolver

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	neturl "net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const (
	defaultVirtualTimeBudgetMilliseconds = 15000

	chromeHeadlessFlagKey                             = "headless"
	chromeHeadlessModeNewValue                        = "new"
	chromeDisableGPUFlagKey                           = "disable-gpu"
	chromeDisableGPUStartupFlagKey                    = "disable-gpu-startup"
	chromeDisableDevShmUsageFlagKey                   = "disable-dev-shm-usage"
	chromeUseGLFlagKey                                = "use-gl"
	chromeUseGLSwiftShaderValue                       = "swiftshader"
	chromeEnableUnsafeSwiftShaderFlag                 = "enable-unsafe-swiftshader"
	chromeNoSandboxFlagKey                            = "no-sandbox"
	chromeDisableSetuidSandboxFlagKey                 = "disable-setuid-sandbox"
	chromeEnableAutomationFlagKey                     = "enable-automation"
	chromeDisableBlinkFeaturesFlagKey                 = "disable-blink-features"
	chromeAutomationControlledBlinkValue              = "AutomationControlled"
	chromeDisableExtensionsFlagKey                    = "disable-extensions"
	chromeDisableComponentExtensionsBackgroundFlagKey = "disable-component-extensions-with-background-pages"
	chromeHideScrollbarsFlagKey                       = "hide-scrollbars"
	chromeNoFirstRunFlagKey                           = "no-first-run"
	chromeNoDefaultBrowserCheckFlagKey                = "no-default-browser-check"
	chromeLogLevelFlagKey                             = "log-level"
	chromeSilentFlagKey                               = "silent"
	chromeDisableLoggingFlagKey                       = "disable-logging"
	chromeIgnoreCertificateErrorsFlag                 = "ignore-certificate-errors"
	chromeUserAgentFlagKey                            = "user-agent"
	chromeVirtualTimeBudgetFlagKey                    = "virtual-time-budget"
	chromeProxyServerFlagKey                          = "proxy-server"
	httpsProxyEnvironmentUpper                        = "HTTPS_PROXY"
	httpsProxyEnvironmentLower                        = "https_proxy"
	httpProxyEnvironmentUpper                         = "HTTP_PROXY"
	httpProxyEnvironmentLower                         = "http_proxy"
	allProxyEnvironmentUpper                          = "ALL_PROXY"
	allProxyEnvironmentLower                          = "all_proxy"
	noProxyEnvironmentUpper                           = "NO_PROXY"
	noProxyEnvironmentLower                           = "no_proxy"
	chromeSilentLogLevelValue                         = "3"
	chromeRendererEmptyURLErrorMessage                = "empty url"
	chromeLogNavigationStartMessage                   = "chromedp navigate: user-agent=%q url=%s"
	chromeLogNavigationSuccessMessage                 = "chromedp render success: url=%s bytes=%d"
	chromeLogNavigationErrorMessage                   = "chromedp render failure: url=%s err=%v"
	chromeLogNetworkRequestMessage                    = "chromedp network request: url=%s"
	chromeLogNetworkResponseMessage                   = "chromedp network response: url=%s status=%d"
	chromeLogNetworkFailureMessage                    = "chromedp network failure: url=%s error=%s canceled=%v"
	chromeLogTargetCrashMessage                       = "chromedp target crashed"

	acceptLanguageHeaderName           = "Accept-Language"
	acceptLanguageHeaderValue          = "en-US,en;q=0.9"
	upgradeInsecureRequestsHeaderName  = "Upgrade-Insecure-Requests"
	upgradeInsecureRequestsHeaderValue = "1"

	documentReadyStateScript             = "document.readyState"
	documentReadyStateCompleteValue      = "complete"
	documentReadyStatePollInterval       = 100 * time.Millisecond
	documentOuterHTMLScript              = "document.documentElement.outerHTML"
	documentOuterHTMLNilDestinationError = "html destination pointer is nil"

	navigatorPlatformMacValue     = "MacIntel"
	navigatorPlatformWindowsValue = "Win32"
	navigatorPlatformLinuxValue   = "Linux x86_64"

	navigatorWebdriverOverrideScript    = "Object.defineProperty(navigator, 'webdriver', { get: () => undefined });"
	navigatorLanguagesOverrideScript    = "Object.defineProperty(navigator, 'languages', { get: () => ['en-US','en'] });"
	navigatorPluginsOverrideScript      = "Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });"
	windowChromeRuntimeDefinitionScript = "window.chrome = window.chrome || {}; window.chrome.runtime = {};"
	navigatorPermissionsOverrideScript  = "const originalQuery = window.navigator.permissions.query; window.navigator.permissions.query = (parameters) => (parameters && parameters.name === 'notifications' ? Promise.resolve({ state: 'default' }) : originalQuery(parameters));"

	userAgentChromeMarker           = "Chrome/"
	userAgentMacintoshToken         = "macintosh"
	userAgentWindowsToken           = "windows"
	userAgentLinuxToken             = "linux"
	userAgentMacVersionToken        = "Mac OS X "
	userAgentWindowsVersionToken    = "Windows NT "
	userAgentTokenUnderscore        = "_"
	userAgentPlatformMacOS          = "macOS"
	userAgentPlatformWindows        = "Windows"
	userAgentPlatformLinux          = "Linux"
	userAgentPlatformVersionDefault = "0.0.0"
	userAgentArchitectureX86        = "x86"
	userAgentBitness64              = "64"
	userAgentWow64Token             = "wow64"
	versionDelimiterSpaceRune       = ' '
	versionDelimiterSemicolonRune   = ';'
	versionDelimiterParenRune       = ')'

	chromeBrandNotABrandName    = "Not A(Brand"
	chromeBrandNotABrandVersion = "8"
	chromeBrandChromiumName     = "Chromium"
	chromeBrandGoogleChromeName = "Google Chrome"
)

var stealthScripts = []string{
	navigatorWebdriverOverrideScript,
	windowChromeRuntimeDefinitionScript,
	navigatorLanguagesOverrideScript,
	navigatorPluginsOverrideScript,
	navigatorPermissionsOverrideScript,
}

// Config controls resolver behavior. Suitable for CLI & Web usage.
type Config struct {
	ChromePath          string // path to Chrome/Chromium binary
	VirtualTimeBudgetMS int    // headless Chrome --virtual-time-budget (ms)

	PerIDTimeout   time.Duration // timeout per ID
	AttemptTimeout time.Duration // timeout per single render attempt (<= PerIDTimeout), optional

	// Request pacing (between IDs)
	Delay       time.Duration // base delay between requests
	Jitter      time.Duration // uniform jitter in [-Jitter, +Jitter]
	BurstSize   int           // 0 disables
	BurstRest   time.Duration // rest after each burst
	BurstJitter time.Duration // jitter for BurstRest

	// Robustness / retries (within the same ID)
	Retries  int           // number of additional attempts (0 = single attempt)
	RetryMin time.Duration // min backoff between attempts
	RetryMax time.Duration // max backoff between attempts

	// UA rotation
	UserAgents []string // rotate per request; if empty, DefaultUAs used

	// Optional debug logger; if nil, no logs.
	Logf func(format string, args ...any)
}

// Request is the payload for batch resolution.
type Request struct {
	IDs []string
}

// Profile is the result for a single ID.
type Profile struct {
	ID          string
	Handle      string
	DisplayName string
	FromURL     string
	Err         string // empty if success
}

// Renderer abstracts how HTML is obtained (exec Chrome vs. mock in tests).
type Renderer interface {
	Render(ctx context.Context, userAgent, url string, vtBudgetMS int, chromePath string) (string, error)
}

// ChromeRenderer uses a headless Chrome process.
type ChromeRenderer struct{}

func NewChromeRenderer() *ChromeRenderer { return &ChromeRenderer{} }

func (renderer *ChromeRenderer) Render(ctx context.Context, userAgent, url string, vtBudgetMS int, chromePath string) (string, error) {
	effectiveBudget := vtBudgetMS
	if effectiveBudget <= 0 {
		effectiveBudget = defaultVirtualTimeBudgetMilliseconds
	}

	trimmedURL := strings.TrimSpace(url)
	if trimmedURL == "" {
		return "", fmt.Errorf(chromeRendererEmptyURLErrorMessage)
	}

	trimmedUserAgent := strings.TrimSpace(userAgent)
	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions,
		chromedp.Flag(chromeHeadlessFlagKey, true),
		chromedp.Flag(chromeDisableGPUFlagKey, true),
		chromedp.Flag(chromeDisableGPUStartupFlagKey, true),
		chromedp.Flag(chromeDisableDevShmUsageFlagKey, true),
		chromedp.Flag(chromeUseGLFlagKey, chromeUseGLSwiftShaderValue),
		chromedp.Flag(chromeEnableUnsafeSwiftShaderFlag, true),
		chromedp.Flag(chromeNoSandboxFlagKey, true),
		chromedp.Flag(chromeDisableSetuidSandboxFlagKey, true),
		chromedp.Flag(chromeRemoteAllowOriginsFlagKey, chromeRemoteAllowOriginsValue),
		chromedp.Flag(chromeHideScrollbarsFlagKey, true),
		chromedp.Flag(chromeNoFirstRunFlagKey, true),
		chromedp.Flag(chromeNoDefaultBrowserCheckFlagKey, true),
		chromedp.Flag(chromeLogLevelFlagKey, chromeSilentLogLevelValue),
		chromedp.Flag(chromeSilentFlagKey, true),
		chromedp.Flag(chromeDisableLoggingFlagKey, true),
		chromedp.Flag(chromeIgnoreCertificateErrorsFlag, true),
		chromedp.Flag(chromeVirtualTimeBudgetFlagKey, strconv.Itoa(effectiveBudget)),
		chromedp.Flag(chromeEnableAutomationFlagKey, false),
		chromedp.Flag(chromeDisableBlinkFeaturesFlagKey, chromeAutomationControlledBlinkValue),
		chromedp.Flag(chromeDisableExtensionsFlagKey, true),
		chromedp.Flag(chromeDisableComponentExtensionsBackgroundFlagKey, true),
	)

	if proxyValue := chromeProxyServerValue(trimmedURL); proxyValue != "" {
		allocatorOptions = append(allocatorOptions, chromedp.Flag(chromeProxyServerFlagKey, proxyValue))
	}

	if trimmedUserAgent != "" {
		allocatorOptions = append(allocatorOptions, chromedp.Flag(chromeUserAgentFlagKey, trimmedUserAgent))
	}

	trimmedChromePath := strings.TrimSpace(chromePath)
	if trimmedChromePath != "" {
		allocatorOptions = append(allocatorOptions, chromedp.ExecPath(trimmedChromePath))
	}

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()

	chromeLogPrinter := logFunctionFromContext(ctx)
	contextOptions := []chromedp.ContextOption{}
	if chromeLogPrinter != nil {
		contextOptions = append(contextOptions,
			chromedp.WithLogf(chromeLogPrinter),
			chromedp.WithErrorf(chromeLogPrinter),
			chromedp.WithDebugf(chromeLogPrinter),
		)
	}

	chromeCtx, cancelChrome := chromedp.NewContext(allocatorCtx, contextOptions...)
	defer cancelChrome()

	if chromeLogPrinter != nil {
		requestURLByID := map[network.RequestID]string{}
		var requestMapMutex sync.Mutex
		chromedp.ListenTarget(chromeCtx, func(event any) {
			switch typedEvent := event.(type) {
			case *network.EventRequestWillBeSent:
				requestMapMutex.Lock()
				requestURLByID[typedEvent.RequestID] = typedEvent.Request.URL
				requestMapMutex.Unlock()
				chromeLogPrinter(chromeLogNetworkRequestMessage, typedEvent.Request.URL)
			case *network.EventResponseReceived:
				requestMapMutex.Lock()
				requestURL := requestURLByID[typedEvent.RequestID]
				if requestURL == "" {
					requestURL = typedEvent.Response.URL
				}
				requestMapMutex.Unlock()
				chromeLogPrinter(chromeLogNetworkResponseMessage, requestURL, int(typedEvent.Response.Status))
			case *network.EventLoadingFailed:
				requestMapMutex.Lock()
				requestURL := requestURLByID[typedEvent.RequestID]
				requestMapMutex.Unlock()
				chromeLogPrinter(chromeLogNetworkFailureMessage, requestURL, typedEvent.ErrorText, typedEvent.Canceled)
			case *target.EventTargetCrashed:
				chromeLogPrinter(chromeLogTargetCrashMessage)
			}
		})
	}

	var htmlContent string
	renderTasks := chromedp.Tasks{
		chromedp.ActionFunc(enableNetworkAndSetHeaders),
		chromedp.ActionFunc(disableAutomationDetection),
		chromedp.ActionFunc(applyStealthScripts),
	}
	if trimmedUserAgent != "" {
		userAgentValue := trimmedUserAgent
		renderTasks = append(renderTasks, chromedp.ActionFunc(func(chromedpCtx context.Context) error {
			return applyUserAgentOverride(chromedpCtx, userAgentValue)
		}))
	}
	if chromeLogPrinter != nil {
		chromeLogPrinter(chromeLogNavigationStartMessage, trimmedUserAgent, trimmedURL)
	}
	renderTasks = append(renderTasks,
		chromedp.Navigate(trimmedURL),
		chromedp.ActionFunc(waitForDocumentReadyStateComplete),
		chromedp.ActionFunc(func(chromedpCtx context.Context) error {
			return readDocumentOuterHTML(chromedpCtx, &htmlContent)
		}),
	)

	if err := chromedp.Run(chromeCtx, renderTasks...); err != nil {
		if chromeLogPrinter != nil {
			chromeLogPrinter(chromeLogNavigationErrorMessage, trimmedURL, err)
		}
		return "", err
	}
	if chromeLogPrinter != nil {
		chromeLogPrinter(chromeLogNavigationSuccessMessage, trimmedURL, len(htmlContent))
	}
	return htmlContent, nil
}

// Service resolves X/Twitter user IDs to handles (and display names).
type Service struct {
	cfg      Config
	rndMu    sync.Mutex
	rnd      *rand.Rand
	renderer Renderer
}

// NewService creates a resolver service using the given renderer (pass nil for default ChromeRenderer).
func NewService(cfg Config, renderer Renderer) *Service {
	seed := time.Now().UnixNano()
	if renderer == nil {
		renderer = NewChromeRenderer()
	}
	return &Service{
		cfg:      cfg,
		rnd:      rand.New(rand.NewSource(seed)),
		renderer: renderer,
	}
}

// ResolveBatch resolves all IDs in-order using a single network funnel with pacing.
// It returns one Profile per input ID (same order).
func (s *Service) ResolveBatch(ctx context.Context, req Request) []Profile {
	results := make([]Profile, 0, len(req.IDs))
	processed := 0

	for _, id := range req.IDs {
		select {
		case <-ctx.Done():
			return results
		default:
		}

		perIDCtx := ctx
		var cancel context.CancelFunc
		if s.cfg.PerIDTimeout > 0 {
			perIDCtx, cancel = context.WithTimeout(ctx, s.cfg.PerIDTimeout)
		}
		started := time.Now()
		if s.cfg.Logf != nil {
			s.cfg.Logf("id=%s start (per-id timeout=%v)", id, s.cfg.PerIDTimeout)
		}

		pro := s.resolveWithRetries(perIDCtx, id)
		results = append(results, pro)

		if s.cfg.Logf != nil {
			s.cfg.Logf("id=%s done in %v err=%v", id, time.Since(started), condErr(pro.Err))
		}
		if cancel != nil {
			cancel() // immediate cancel per-id (don't defer across loop)
		}

		processed++

		// per-request pacing with jitter
		if sleep := s.jitterDuration(s.cfg.Delay, s.cfg.Jitter); sleep > 0 {
			if !s.sleepCtx(ctx, sleep) {
				return results
			}
		}

		// burst rest
		if s.cfg.BurstSize > 0 && processed%s.cfg.BurstSize == 0 {
			if rest := s.jitterDuration(s.cfg.BurstRest, s.cfg.BurstJitter); rest > 0 {
				if !s.sleepCtx(ctx, rest) {
					return results
				}
			}
		}
	}
	return results
}

func (s *Service) resolveWithRetries(ctx context.Context, id string) Profile {
	candidates := []string{
		"https://x.com/intent/user?user_id=" + id,
		"https://x.com/i/user/" + id,
	}

	attempts := s.cfg.Retries + 1
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		for _, url := range candidates {
			select {
			case <-ctx.Done():
				return Profile{ID: id, FromURL: url, Err: ctx.Err().Error()}
			default:
			}

			ua := s.pickUA()
			if s.cfg.Logf != nil {
				s.cfg.Logf("id=%s attempt=%d url=%s ua=%q", id, attempt+1, url, ua)
			}

			// Per-attempt timeout nests under per-ID timeout.
			attemptCtx := ctx
			var cancel context.CancelFunc
			if s.cfg.AttemptTimeout > 0 {
				attemptCtx, cancel = context.WithTimeout(ctx, s.cfg.AttemptTimeout)
			}
			renderCtx := attemptCtx
			if s.cfg.Logf != nil {
				renderCtx = withLogFunction(attemptCtx, s.cfg.Logf)
			}
			started := time.Now()
			htmlDoc, err := s.renderer.Render(renderCtx, ua, url, s.cfg.VirtualTimeBudgetMS, s.cfg.ChromePath)
			if cancel != nil {
				cancel()
			}
			if err != nil || strings.TrimSpace(htmlDoc) == "" {
				if s.cfg.Logf != nil {
					s.cfg.Logf("id=%s attempt=%d url=%s elapsed=%v err=%v empty=%v",
						id, attempt+1, url, time.Since(started), condErr(errStr(err)), strings.TrimSpace(htmlDoc) == "")
				}
				if err != nil {
					lastErr = err
				} else {
					lastErr = fmt.Errorf("empty document")
				}
				continue
			}

			normalized := strings.ReplaceAll(htmlDoc, `'`, `"`)
			handle := extractHandle(normalized)
			display := extractDisplayName(normalized, handle)
			if handle != "" {
				if s.cfg.Logf != nil {
					s.cfg.Logf("id=%s attempt=%d url=%s elapsed=%v OK handle=%s",
						id, attempt+1, url, time.Since(started), handle)
				}
				return Profile{
					ID:          id,
					Handle:      handle,
					DisplayName: display,
					FromURL:     url,
				}
			}
			lastErr = fmt.Errorf("no handle found")
		}

		// backoff before next attempt if we still have time
		if attempt < attempts-1 {
			sleep := s.backoffDuration(attempt)
			if sleep > 0 && !s.sleepCtx(ctx, sleep) {
				return Profile{ID: id, Err: ctx.Err().Error()}
			}
		}
	}
	msg := "unresolvable"
	if lastErr != nil {
		msg = lastErr.Error()
	}
	return Profile{ID: id, Err: msg}
}

func (s *Service) pickUA() string {
	if len(s.cfg.UserAgents) == 0 {
		return s.defaultChromeUserAgent()
	}
	idx := s.randIntn(len(s.cfg.UserAgents))
	return s.cfg.UserAgents[idx]
}

// DefaultChromeUserAgent returns a reasonable UA when none provided.
func DefaultChromeUserAgent(r *rand.Rand) string {
	if r == nil {
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return DefaultUAs[r.Intn(len(DefaultUAs))]
}

func (s *Service) jitterDuration(base, jitter time.Duration) time.Duration {
	if base < 0 {
		base = 0
	}
	if jitter <= 0 {
		return base
	}
	offset := (s.randFloat64()*2 - 1) * float64(jitter)
	d := time.Duration(float64(base) + offset)
	if d < 0 {
		return 0
	}
	return d
}

func (s *Service) sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// Simple backoff between retry attempts: grow from RetryMin toward RetryMax.
func (s *Service) backoffDuration(attempt int) time.Duration {
	min := s.cfg.RetryMin
	max := s.cfg.RetryMax
	if min <= 0 && max <= 0 {
		// sensible default
		min, max = 400*time.Millisecond, 1500*time.Millisecond
	}
	if min <= 0 {
		min = max / 2
	}
	if max < min {
		max = min
	}
	// exponential-like growth clipped to [min,max]
	scale := 1.0 + float64(attempt)
	d := time.Duration(float64(min) * scale)
	if d > max {
		d = max
	}
	// add small jitter (+/- 25%)
	j := time.Duration(0.25 * float64(d))
	offset := (s.randFloat64()*2 - 1) * float64(j)
	return time.Duration(float64(d) + offset)
}

func (s *Service) defaultChromeUserAgent() string {
	s.rndMu.Lock()
	defer s.rndMu.Unlock()
	return DefaultChromeUserAgent(s.rnd)
}

func (s *Service) randFloat64() float64 {
	s.rndMu.Lock()
	defer s.rndMu.Unlock()
	if s.rnd == nil {
		return rand.Float64()
	}
	return s.rnd.Float64()
}

func (s *Service) randIntn(n int) int {
	s.rndMu.Lock()
	defer s.rndMu.Unlock()
	if s.rnd == nil {
		return rand.Intn(n)
	}
	return s.rnd.Intn(n)
}

// ===== Helpers (pure) =====

func extractHandle(htmlDoc string) string {
	for _, full := range ProfileURLRegex.FindAllString(htmlDoc, -1) {
		h := stripDomainPrefix(full)
		if !isReserved(h) {
			return h
		}
	}
	return ""
}

func extractDisplayName(htmlDoc string, handle string) string {
	title := firstGroup(MetaOGTitle.FindStringSubmatch(htmlDoc))
	if title == "" {
		title = firstGroup(MetaTitleTag.FindStringSubmatch(htmlDoc))
	}
	if title == "" {
		return ""
	}
	if idx := strings.Index(title, "(@"); idx > 0 {
		return strings.TrimSpace(title[:idx])
	}
	name := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(title, " / X"), " on X"))
	if handle != "" {
		name = strings.ReplaceAll(name, "(@"+handle+")", "")
		name = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(name, " / X"), " on X"))
	}
	return strings.TrimSpace(name)
}

func stripDomainPrefix(fullURL string) string {
	fullURL = strings.TrimPrefix(fullURL, "https://")
	if i := strings.IndexByte(fullURL, '/'); i >= 0 {
		return fullURL[i+1:]
	}
	return fullURL
}

func isReserved(handle string) bool {
	_, bad := ReservedTopLevelPaths[strings.ToLower(handle)]
	return bad
}

func firstGroup(m []string) string {
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

func enableNetworkAndSetHeaders(chromedpCtx context.Context) error {
	if err := network.Enable().Do(chromedpCtx); err != nil {
		return err
	}
	headers := network.Headers{
		acceptLanguageHeaderName:          acceptLanguageHeaderValue,
		upgradeInsecureRequestsHeaderName: upgradeInsecureRequestsHeaderValue,
	}
	return network.SetExtraHTTPHeaders(headers).Do(chromedpCtx)
}

func disableAutomationDetection(chromedpCtx context.Context) error {
	return emulation.SetAutomationOverride(false).Do(chromedpCtx)
}

func applyStealthScripts(chromedpCtx context.Context) error {
	for _, script := range stealthScripts {
		if _, err := page.AddScriptToEvaluateOnNewDocument(script).Do(chromedpCtx); err != nil {
			return err
		}
	}
	return nil
}

func applyUserAgentOverride(chromedpCtx context.Context, userAgent string) error {
	userAgentOverride := emulation.SetUserAgentOverride(userAgent).WithAcceptLanguage(acceptLanguageHeaderValue)
	navigatorPlatform := navigatorPlatformForUserAgent(userAgent)
	if navigatorPlatform != "" {
		userAgentOverride = userAgentOverride.WithPlatform(navigatorPlatform)
	}
	if metadata := userAgentMetadataFromUserAgent(userAgent); metadata != nil {
		userAgentOverride = userAgentOverride.WithUserAgentMetadata(metadata)
	}
	return userAgentOverride.Do(chromedpCtx)
}

func waitForDocumentReadyStateComplete(chromedpCtx context.Context) error {
	ticker := time.NewTicker(documentReadyStatePollInterval)
	defer ticker.Stop()

	var lastEvaluationError error
	for {
		var readyStateValue string
		evaluationErr := chromedp.Evaluate(documentReadyStateScript, &readyStateValue, chromedp.EvalAsValue).Do(chromedpCtx)
		if evaluationErr == nil {
			lastEvaluationError = nil
			if strings.EqualFold(strings.TrimSpace(readyStateValue), documentReadyStateCompleteValue) {
				return nil
			}
		} else {
			lastEvaluationError = evaluationErr
		}

		select {
		case <-chromedpCtx.Done():
			if lastEvaluationError != nil {
				return lastEvaluationError
			}
			return chromedpCtx.Err()
		case <-ticker.C:
		}
	}
}

func readDocumentOuterHTML(chromedpCtx context.Context, htmlContentDestination *string) error {
	if htmlContentDestination == nil {
		return fmt.Errorf(documentOuterHTMLNilDestinationError)
	}

	var documentOuterHTML string
	if err := chromedp.Evaluate(documentOuterHTMLScript, &documentOuterHTML, chromedp.EvalAsValue).Do(chromedpCtx); err != nil {
		return err
	}
	*htmlContentDestination = documentOuterHTML
	return nil
}

func navigatorPlatformForUserAgent(userAgent string) string {
	normalized := strings.ToLower(userAgent)
	switch {
	case strings.Contains(normalized, userAgentMacintoshToken):
		return navigatorPlatformMacValue
	case strings.Contains(normalized, userAgentWindowsToken):
		return navigatorPlatformWindowsValue
	case strings.Contains(normalized, userAgentLinuxToken):
		return navigatorPlatformLinuxValue
	default:
		return navigatorPlatformMacValue
	}
}

func userAgentMetadataFromUserAgent(userAgent string) *emulation.UserAgentMetadata {
	majorVersion, fullVersion := extractChromeVersions(userAgent)
	if majorVersion == "" || fullVersion == "" {
		return nil
	}
	platformDetails := platformDetailsFromUserAgent(userAgent)
	brandVersions := majorBrandVersions(majorVersion)
	fullVersionList := fullVersionBrandList(fullVersion)
	return &emulation.UserAgentMetadata{
		Brands:          brandVersions,
		FullVersionList: fullVersionList,
		Platform:        platformDetails.platform,
		PlatformVersion: platformDetails.platformVersion,
		Architecture:    platformDetails.architecture,
		Model:           platformDetails.model,
		Mobile:          false,
		Bitness:         platformDetails.bitness,
		Wow64:           platformDetails.wow64,
	}
}

type userAgentPlatformDetails struct {
	platform        string
	platformVersion string
	architecture    string
	bitness         string
	model           string
	wow64           bool
}

func platformDetailsFromUserAgent(userAgent string) userAgentPlatformDetails {
	normalized := strings.ToLower(userAgent)
	details := userAgentPlatformDetails{
		platform:        userAgentPlatformMacOS,
		platformVersion: userAgentPlatformVersionDefault,
		architecture:    userAgentArchitectureX86,
		bitness:         userAgentBitness64,
		model:           "",
		wow64:           false,
	}
	switch {
	case strings.Contains(normalized, userAgentMacintoshToken):
		details.platform = userAgentPlatformMacOS
		details.platformVersion = macPlatformVersion(userAgent)
	case strings.Contains(normalized, userAgentWindowsToken):
		details.platform = userAgentPlatformWindows
		details.platformVersion = windowsPlatformVersion(userAgent)
		details.wow64 = strings.Contains(normalized, userAgentWow64Token)
	case strings.Contains(normalized, userAgentLinuxToken):
		details.platform = userAgentPlatformLinux
		details.platformVersion = userAgentPlatformVersionDefault
	default:
		details.platform = userAgentPlatformMacOS
		details.platformVersion = userAgentPlatformVersionDefault
	}
	return details
}

func extractChromeVersions(userAgent string) (string, string) {
	markerIndex := strings.Index(userAgent, userAgentChromeMarker)
	if markerIndex < 0 {
		return "", ""
	}
	versionSection := userAgent[markerIndex+len(userAgentChromeMarker):]
	delimiterIndex := indexOfVersionDelimiter(versionSection)
	versionValue := strings.TrimSpace(versionSection[:delimiterIndex])
	if versionValue == "" {
		return "", ""
	}
	majorVersion := versionValue
	if dotIndex := strings.Index(versionValue, "."); dotIndex >= 0 {
		majorVersion = versionValue[:dotIndex]
	}
	return majorVersion, versionValue
}

func macPlatformVersion(userAgent string) string {
	rawVersion := parseVersionAfterToken(userAgent, userAgentMacVersionToken)
	if rawVersion == "" {
		return userAgentPlatformVersionDefault
	}
	normalizedVersion := strings.ReplaceAll(rawVersion, userAgentTokenUnderscore, ".")
	return canonicalizeVersion(normalizedVersion)
}

func windowsPlatformVersion(userAgent string) string {
	rawVersion := parseVersionAfterToken(userAgent, userAgentWindowsVersionToken)
	if rawVersion == "" {
		return userAgentPlatformVersionDefault
	}
	return canonicalizeVersion(rawVersion)
}

func parseVersionAfterToken(userAgent string, token string) string {
	startIndex := strings.Index(userAgent, token)
	if startIndex < 0 {
		return ""
	}
	versionSection := userAgent[startIndex+len(token):]
	delimiterIndex := indexOfVersionDelimiter(versionSection)
	versionCandidate := strings.TrimSpace(versionSection[:delimiterIndex])
	return versionCandidate
}

func indexOfVersionDelimiter(versionSection string) int {
	for index, charValue := range versionSection {
		if charValue == versionDelimiterSpaceRune || charValue == versionDelimiterSemicolonRune || charValue == versionDelimiterParenRune {
			return index
		}
	}
	return len(versionSection)
}

func canonicalizeVersion(version string) string {
	if version == "" {
		return userAgentPlatformVersionDefault
	}
	components := strings.Split(version, ".")
	for len(components) < 3 {
		components = append(components, "0")
	}
	if len(components) > 3 {
		components = components[:3]
	}
	return strings.Join(components, ".")
}

func majorBrandVersions(majorVersion string) []*emulation.UserAgentBrandVersion {
	if majorVersion == "" {
		return nil
	}
	return []*emulation.UserAgentBrandVersion{
		buildBrandVersion(chromeBrandNotABrandName, chromeBrandNotABrandVersion),
		buildBrandVersion(chromeBrandChromiumName, majorVersion),
		buildBrandVersion(chromeBrandGoogleChromeName, majorVersion),
	}
}

func fullVersionBrandList(fullVersion string) []*emulation.UserAgentBrandVersion {
	if fullVersion == "" {
		return nil
	}
	return []*emulation.UserAgentBrandVersion{
		buildBrandVersion(chromeBrandChromiumName, fullVersion),
		buildBrandVersion(chromeBrandGoogleChromeName, fullVersion),
	}
}

func buildBrandVersion(brand string, version string) *emulation.UserAgentBrandVersion {
	return &emulation.UserAgentBrandVersion{Brand: brand, Version: version}
}

func chromeProxyServerValue(targetURL string) string {
	normalizedURL := strings.TrimSpace(targetURL)
	if normalizedURL == "" {
		return ""
	}

	parsedURL, parseErr := neturl.Parse(normalizedURL)
	if parseErr != nil {
		return ""
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return ""
	}

	proxyCandidate := firstNonEmptyEnvValue(proxyEnvironmentKeys(parsedURL.Scheme)...)
	if proxyCandidate == "" && parsedURL.Scheme == "https" {
		proxyCandidate = firstNonEmptyEnvValue(proxyEnvironmentKeys("http")...)
	}
	if proxyCandidate == "" {
		proxyCandidate = firstNonEmptyEnvValue(allProxyEnvironmentUpper, allProxyEnvironmentLower)
	}
	if proxyCandidate == "" {
		return ""
	}

	if bypassProxy(parsedURL, firstNonEmptyEnvValue(noProxyEnvironmentUpper, noProxyEnvironmentLower)) {
		return ""
	}

	parsedProxyURL, proxyParseErr := neturl.Parse(proxyCandidate)
	if proxyParseErr != nil || strings.TrimSpace(parsedProxyURL.Host) == "" {
		return ""
	}
	return proxyCandidate
}

func proxyEnvironmentKeys(scheme string) []string {
	switch scheme {
	case "https":
		return []string{httpsProxyEnvironmentUpper, httpsProxyEnvironmentLower}
	case "http":
		return []string{httpProxyEnvironmentUpper, httpProxyEnvironmentLower}
	default:
		return nil
	}
}

func firstNonEmptyEnvValue(environmentKeys ...string) string {
	for _, key := range environmentKeys {
		if key == "" {
			continue
		}
		if value, present := os.LookupEnv(key); present {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func bypassProxy(targetURL *neturl.URL, noProxyList string) bool {
	if strings.TrimSpace(noProxyList) == "" {
		return false
	}
	host := strings.ToLower(targetURL.Hostname())
	port := targetURL.Port()
	if port == "" {
		port = defaultPortForScheme(targetURL.Scheme)
	}
	if host == "" {
		return false
	}

	entries := strings.Split(noProxyList, ",")
	for _, entry := range entries {
		trimmedEntry := strings.TrimSpace(entry)
		if trimmedEntry == "" {
			continue
		}
		if trimmedEntry == "*" {
			return true
		}

		entryHost := trimmedEntry
		entryPort := ""
		if strings.Contains(trimmedEntry, ":") {
			parsedHost, parsedPort, splitErr := net.SplitHostPort(trimmedEntry)
			if splitErr == nil {
				entryHost = parsedHost
				entryPort = parsedPort
			}
		}

		normalizedEntryHost := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(entryHost, "*"), "."))
		if normalizedEntryHost == "" {
			continue
		}
		if entryPort != "" && entryPort != port {
			continue
		}

		if host == normalizedEntryHost {
			return true
		}
		if strings.HasSuffix(host, "."+normalizedEntryHost) {
			return true
		}
	}
	return false
}

func defaultPortForScheme(scheme string) string {
	switch strings.ToLower(scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func condErr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

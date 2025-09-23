package tests

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/f-sync/fsync/internal/utils/chromepath"
	"github.com/f-sync/fsync/internal/xresolver"
)

const (
	xresolverIntegrationFlagName                 = "xresolver_integration"
	xresolverIntegrationFlagDescription          = "enable live xresolver integration test with Chrome handle resolution"
	xresolverIntegrationFlagDisabledMessage      = "xresolver integration test skipped because the flag is disabled"
	xresolverIntegrationChromeUnavailableMessage = "xresolver integration test skipped because no Chrome binary is available"

	xresolverIntegrationScenarioResolveElon = "resolve elon musk via chrome renderer"

	xresolverIntegrationAccountIDElon        = "44196397"
	xresolverIntegrationExpectedHandleElon   = "elonmusk"
	xresolverIntegrationExpectedDisplayElon  = "Elon Musk"
	xresolverIntegrationExpectedProfileCount = 1

	xresolverIntegrationUnexpectedProfileCountFormat = "expected %d profile results, got %d"
	xresolverIntegrationUnexpectedResolveErrorFormat = "unexpected resolve error for account %s: %s"
	xresolverIntegrationUnexpectedHandleFormat       = "expected handle %q for account %s, got %q"
	xresolverIntegrationUnexpectedDisplayNameFormat  = "expected display name %q for account %s, got %q"
	xresolverIntegrationEmptySourceURLFormat         = "expected non-empty source URL for account %s"
	xresolverIntegrationFallbackErrorFormat          = "fallback chrome discovery failed (fallback error: %v, initial error: %v)"
	xresolverIntegrationUnexpectedAccountIDFormat    = "expected account id %s, got %s"
)

const xresolverIntegrationContextTimeout = 2 * time.Minute

var xresolverIntegrationRunFlag = flag.Bool(xresolverIntegrationFlagName, false, xresolverIntegrationFlagDescription)

type xresolverIntegrationScenario struct {
	scenarioName        string
	accountID           string
	expectedHandle      string
	expectedDisplayName string
}

func TestXResolverChromeIntegration(t *testing.T) {
	if !*xresolverIntegrationRunFlag {
		t.Skip(xresolverIntegrationFlagDisabledMessage)
	}

	chromeBinaryPath, chromeErr := resolveChromeBinaryPathForXResolverIntegration()
	if chromeErr != nil {
		t.Skipf(integrationSkipMessageFormat, xresolverIntegrationChromeUnavailableMessage, chromeErr)
	}

	integrationService := xresolver.NewService(xresolver.Config{ChromePath: chromeBinaryPath}, xresolver.NewChromeRenderer())

	scenarios := []xresolverIntegrationScenario{
		{
			scenarioName:        xresolverIntegrationScenarioResolveElon,
			accountID:           xresolverIntegrationAccountIDElon,
			expectedHandle:      xresolverIntegrationExpectedHandleElon,
			expectedDisplayName: xresolverIntegrationExpectedDisplayElon,
		},
	}

	for _, scenario := range scenarios {
		scenario := scenario
		t.Run(scenario.scenarioName, func(t *testing.T) {
			testContext, cancelTestContext := context.WithTimeout(context.Background(), xresolverIntegrationContextTimeout)
			defer cancelTestContext()

			request := xresolver.Request{IDs: []string{scenario.accountID}}
			profiles := integrationService.ResolveBatch(testContext, request)
			if len(profiles) != xresolverIntegrationExpectedProfileCount {
				t.Fatalf(xresolverIntegrationUnexpectedProfileCountFormat, xresolverIntegrationExpectedProfileCount, len(profiles))
			}

			resolvedProfile := profiles[0]
			if resolvedProfile.ID != scenario.accountID {
				t.Fatalf(xresolverIntegrationUnexpectedAccountIDFormat, scenario.accountID, resolvedProfile.ID)
			}
			if resolvedProfile.Err != "" {
				t.Fatalf(xresolverIntegrationUnexpectedResolveErrorFormat, scenario.accountID, resolvedProfile.Err)
			}
			if resolvedProfile.Handle != scenario.expectedHandle {
				t.Fatalf(xresolverIntegrationUnexpectedHandleFormat, scenario.expectedHandle, scenario.accountID, resolvedProfile.Handle)
			}
			if resolvedProfile.DisplayName != scenario.expectedDisplayName {
				t.Fatalf(xresolverIntegrationUnexpectedDisplayNameFormat, scenario.expectedDisplayName, scenario.accountID, resolvedProfile.DisplayName)
			}
			if strings.TrimSpace(resolvedProfile.FromURL) == "" {
				t.Fatalf(xresolverIntegrationEmptySourceURLFormat, scenario.accountID)
			}
		})
	}
}

func resolveChromeBinaryPathForXResolverIntegration() (string, error) {
	discoveredPath := chromepath.Discover()
	resolvedPath, resolveErr := chromepath.ResolveExecutablePath(discoveredPath)
	if resolveErr == nil {
		return resolvedPath, nil
	}

	defaultPath := chromepath.DefaultPath()
	if strings.TrimSpace(defaultPath) == strings.TrimSpace(discoveredPath) {
		return "", resolveErr
	}

	fallbackPath, fallbackErr := chromepath.ResolveExecutablePath(defaultPath)
	if fallbackErr != nil {
		return "", fmt.Errorf(xresolverIntegrationFallbackErrorFormat, fallbackErr, resolveErr)
	}
	return fallbackPath, nil
}

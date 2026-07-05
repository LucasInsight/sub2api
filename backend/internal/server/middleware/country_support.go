package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geoip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const countryUnsupportedMessage = "Service is not available in your country or region."

type CountrySupportLookup func(string) (geoip.LookupResult, error)
type GatewayErrorWriterSelector func(*gin.Context) GatewayErrorWriter

var (
	countrySupportLookupMu sync.RWMutex
	countrySupportLookup   CountrySupportLookup = geoip.Lookup
)

// SetCountrySupportLookupForTest overrides the GeoIP lookup used by CountrySupportGate.
func SetCountrySupportLookupForTest(lookup CountrySupportLookup) func() {
	countrySupportLookupMu.Lock()
	previous := countrySupportLookup
	countrySupportLookup = lookup
	countrySupportLookupMu.Unlock()
	return func() {
		countrySupportLookupMu.Lock()
		countrySupportLookup = previous
		countrySupportLookupMu.Unlock()
	}
}

func currentCountrySupportLookup() CountrySupportLookup {
	countrySupportLookupMu.RLock()
	defer countrySupportLookupMu.RUnlock()
	return countrySupportLookup
}

// OpenAIErrorWriter returns the current project OpenAI-compatible error shape.
func OpenAIErrorWriter(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    "permission_error",
			"message": message,
		},
	})
}

// ResponsesErrorWriter returns the current project Responses error shape.
func ResponsesErrorWriter(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":    "permission_error",
			"message": message,
		},
	})
}

// CountrySupportGate blocks gateway requests from configured unsupported countries.
func CountrySupportGate(countrySupport config.CountrySupportConfig, selectWriter GatewayErrorWriterSelector) gin.HandlerFunc {
	return CountrySupportGateWithLookup(countrySupport, selectWriter, currentCountrySupportLookup())
}

func CountrySupportGateWithLookup(
	countrySupport config.CountrySupportConfig,
	selectWriter GatewayErrorWriterSelector,
	lookup CountrySupportLookup,
) gin.HandlerFunc {
	blockedCountryCodes := config.NormalizeCountryCodes(countrySupport.BlockedCountryCodes)
	return func(c *gin.Context) {
		if len(blockedCountryCodes) == 0 {
			c.Next()
			return
		}

		clientIP := ip.GetTrustedClientIP(c)
		if clientIP == "" || isNonPublicIP(clientIP) || lookup == nil {
			c.Next()
			return
		}

		result, err := lookup(clientIP)
		if err != nil {
			logger.L().With(
				zap.String("component", "server.middleware.country_support"),
				zap.String("client_ip", clientIP),
				zap.String("path", c.Request.URL.Path),
				zap.Error(err),
			).Warn("country_support.lookup_failed")
			c.Next()
			return
		}

		countryCode := geoip.EffectiveCountryCode(result)
		if !geoip.IsBlockedCountry(countryCode, blockedCountryCodes) {
			c.Next()
			return
		}

		apiKey, _ := GetAPIKeyFromContext(c)
		platform := gatewayPlatform(apiKey)
		logger.L().With(
			zap.String("component", "server.middleware.country_support"),
			zap.String("client_ip", clientIP),
			zap.String("country_code", countryCode),
			zap.String("path", c.Request.URL.Path),
			zap.Int64("api_key_id", apiKeyID(apiKey)),
			zap.String("platform", platform),
		).Info("country_support.blocked")

		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
		writer := AnthropicErrorWriter
		if selectWriter != nil {
			if selected := selectWriter(c); selected != nil {
				writer = selected
			}
		}
		writer(c, http.StatusForbidden, countryUnsupportedMessage)
		c.Abort()
	}
}

func isNonPublicIP(ipText string) bool {
	ipText = strings.TrimSpace(ipText)
	if ipText == "" {
		return true
	}
	parsed := net.ParseIP(ipText)
	if parsed == nil {
		return true
	}
	return parsed.IsLoopback() ||
		parsed.IsPrivate() ||
		parsed.IsUnspecified() ||
		parsed.IsLinkLocalUnicast() ||
		parsed.IsLinkLocalMulticast() ||
		parsed.IsMulticast()
}

func gatewayPlatform(apiKey *service.APIKey) string {
	if apiKey == nil || apiKey.Group == nil {
		return ""
	}
	return apiKey.Group.Platform
}

func apiKeyID(apiKey *service.APIKey) int64 {
	if apiKey == nil {
		return 0
	}
	return apiKey.ID
}

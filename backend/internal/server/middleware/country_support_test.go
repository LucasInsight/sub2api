package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geoip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCountrySupportGateBlocksUnsupportedCountry(t *testing.T) {
	router, nextCalled := newCountrySupportTestRouter(
		config.CountrySupportConfig{BlockedCountryCodes: []string{"CN"}},
		func(string) (geoip.LookupResult, error) {
			return geoip.LookupResult{CountryCode: "CN"}, nil
		},
		func(*gin.Context) GatewayErrorWriter { return OpenAIErrorWriter },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, *nextCalled)
	require.JSONEq(t, `{"error":{"type":"permission_error","message":"Service is not available in your country or region."}}`, w.Body.String())
	require.NotContains(t, w.Body.String(), "CN")
	require.NotContains(t, w.Body.String(), "8.8.8.8")
}

func TestCountrySupportGateAllowsSupportedCountry(t *testing.T) {
	router, nextCalled := newCountrySupportTestRouter(
		config.CountrySupportConfig{BlockedCountryCodes: []string{"CN"}},
		func(string) (geoip.LookupResult, error) {
			return geoip.LookupResult{CountryCode: "US"}, nil
		},
		nil,
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, *nextCalled)
}

func TestCountrySupportGateAllowsUnknownCountry(t *testing.T) {
	router, nextCalled := newCountrySupportTestRouter(
		config.CountrySupportConfig{BlockedCountryCodes: []string{"CN"}},
		func(string) (geoip.LookupResult, error) {
			return geoip.LookupResult{}, nil
		},
		nil,
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, *nextCalled)
}

func TestCountrySupportGateAllowsLookupError(t *testing.T) {
	router, nextCalled := newCountrySupportTestRouter(
		config.CountrySupportConfig{BlockedCountryCodes: []string{"CN"}},
		func(string) (geoip.LookupResult, error) {
			return geoip.LookupResult{}, errors.New("lookup failed")
		},
		nil,
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, *nextCalled)
}

func TestCountrySupportGateAllowsPrivateIPWithoutLookup(t *testing.T) {
	lookupCalled := false
	router, nextCalled := newCountrySupportTestRouter(
		config.CountrySupportConfig{BlockedCountryCodes: []string{"CN"}},
		func(string) (geoip.LookupResult, error) {
			lookupCalled = true
			return geoip.LookupResult{CountryCode: "CN"}, nil
		},
		nil,
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, *nextCalled)
	require.False(t, lookupCalled)
}

func TestCountrySupportGateAnthropicErrorWriter(t *testing.T) {
	router, _ := newCountrySupportTestRouter(
		config.CountrySupportConfig{BlockedCountryCodes: []string{"CN"}},
		func(string) (geoip.LookupResult, error) {
			return geoip.LookupResult{CountryCode: "CN"}, nil
		},
		func(*gin.Context) GatewayErrorWriter { return AnthropicErrorWriter },
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.JSONEq(t, `{"type":"error","error":{"type":"permission_error","message":"Service is not available in your country or region."}}`, w.Body.String())
	require.NotContains(t, w.Body.String(), "CN")
	require.NotContains(t, w.Body.String(), "8.8.8.8")
}

func newCountrySupportTestRouter(
	countrySupport config.CountrySupportConfig,
	lookup CountrySupportLookup,
	selectWriter GatewayErrorWriterSelector,
) (*gin.Engine, *bool) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	nextCalled := false
	router.Use(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(ContextKeyAPIKey), &service.APIKey{
			ID:      42,
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformOpenAI},
		})
		c.Next()
	})
	router.Use(CountrySupportGateWithLookup(countrySupport, selectWriter, lookup))
	router.POST("/v1/messages", func(c *gin.Context) {
		nextCalled = true
		c.String(http.StatusOK, "ok")
	})
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		nextCalled = true
		c.String(http.StatusOK, "ok")
	})
	return router, &nextCalled
}

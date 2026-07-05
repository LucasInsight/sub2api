package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geoip"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettingHandlerGetCurrentIPGeoSupported(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, "test")
	h.SetCountrySupportConfig(config.CountrySupportConfig{
		BlockedCountryCodes: []string{"RU"},
	})
	h.ipGeoLookup = func(ipText string) (geoip.LookupResult, error) {
		require.Equal(t, "8.8.8.8", ipText)
		return geoip.LookupResult{IP: ipText, CountryCode: "US"}, nil
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ip-geo", nil)
	c.Request.RemoteAddr = "8.8.8.8:12345"

	h.GetCurrentIPGeo(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeCurrentIPGeoResponse(t, recorder)
	require.Equal(t, "8.8.8.8", resp.Data.IP)
	require.Equal(t, "US", resp.Data.CountryCode)
	require.True(t, resp.Data.CountryKnown)
	require.True(t, resp.Data.Supported)
	require.Equal(t, ipGeoSupportStatusSupported, resp.Data.SupportStatus)
}

func TestSettingHandlerGetCurrentIPGeoUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, "test")
	h.SetCountrySupportConfig(config.CountrySupportConfig{
		BlockedCountryCodes: []string{" us "},
	})
	h.ipGeoLookup = func(ipText string) (geoip.LookupResult, error) {
		return geoip.LookupResult{IP: ipText, CountryCode: "US"}, nil
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ip-geo", nil)
	c.Request.RemoteAddr = "8.8.8.8:12345"

	h.GetCurrentIPGeo(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeCurrentIPGeoResponse(t, recorder)
	require.Equal(t, "US", resp.Data.CountryCode)
	require.True(t, resp.Data.CountryKnown)
	require.False(t, resp.Data.Supported)
	require.Equal(t, ipGeoSupportStatusUnsupported, resp.Data.SupportStatus)
}

func TestSettingHandlerGetCurrentIPGeoDefaultsChinaUnsupported(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, "test")
	h.ipGeoLookup = func(ipText string) (geoip.LookupResult, error) {
		return geoip.LookupResult{IP: ipText, CountryCode: "CN", IsChina: true}, nil
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ip-geo", nil)
	c.Request.RemoteAddr = "114.114.114.114:12345"

	h.GetCurrentIPGeo(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeCurrentIPGeoResponse(t, recorder)
	require.Equal(t, "CN", resp.Data.CountryCode)
	require.True(t, resp.Data.CountryKnown)
	require.True(t, resp.Data.IsChina)
	require.False(t, resp.Data.Supported)
	require.Equal(t, ipGeoSupportStatusUnsupported, resp.Data.SupportStatus)
}

func TestSettingHandlerGetCurrentIPGeoUnknown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, "test")
	h.ipGeoLookup = func(ipText string) (geoip.LookupResult, error) {
		return geoip.LookupResult{IP: ipText}, nil
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ip-geo", nil)
	c.Request.RemoteAddr = "127.0.0.1:12345"

	h.GetCurrentIPGeo(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeCurrentIPGeoResponse(t, recorder)
	require.Equal(t, "127.0.0.1", resp.Data.IP)
	require.Empty(t, resp.Data.CountryCode)
	require.False(t, resp.Data.CountryKnown)
	require.False(t, resp.Data.Supported)
	require.Equal(t, ipGeoSupportStatusUnknown, resp.Data.SupportStatus)
}

func TestSettingHandlerGetCurrentIPGeoUsesRegisteredCountryFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, "test")
	h.SetCountrySupportConfig(config.CountrySupportConfig{
		BlockedCountryCodes: []string{"AU"},
	})
	h.ipGeoLookup = func(ipText string) (geoip.LookupResult, error) {
		return geoip.LookupResult{IP: ipText, RegisteredCountryCode: "AU"}, nil
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ip-geo", nil)
	c.Request.RemoteAddr = "1.1.1.1:12345"

	h.GetCurrentIPGeo(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	resp := decodeCurrentIPGeoResponse(t, recorder)
	require.Equal(t, "AU", resp.Data.CountryCode)
	require.Equal(t, "AU", resp.Data.RegisteredCountryCode)
	require.True(t, resp.Data.CountryKnown)
	require.False(t, resp.Data.Supported)
	require.Equal(t, ipGeoSupportStatusUnsupported, resp.Data.SupportStatus)
}

func TestSettingHandlerGetCurrentIPGeoLookupError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(nil, "test")
	h.ipGeoLookup = func(ipText string) (geoip.LookupResult, error) {
		return geoip.LookupResult{}, errors.New("lookup failed")
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/ip-geo", nil)
	c.Request.RemoteAddr = "8.8.8.8:12345"

	h.GetCurrentIPGeo(c)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
}

func decodeCurrentIPGeoResponse(t *testing.T, recorder *httptest.ResponseRecorder) struct {
	Code int                  `json:"code"`
	Data currentIPGeoResponse `json:"data"`
} {
	t.Helper()
	var resp struct {
		Code int                  `json:"code"`
		Data currentIPGeoResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	return resp
}

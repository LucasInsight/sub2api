package geoip

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func testCountryDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "resources", "geoip", "GeoLite2-Country.mmdb")
}

func TestChinaIPCheckerLookup(t *testing.T) {
	checker, err := NewChinaIPChecker(testCountryDBPath(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, checker.Close()) })

	tests := []struct {
		name        string
		ip          string
		wantCountry string
		wantChina   bool
	}{
		{name: "china ipv4 dns", ip: "114.114.114.114", wantCountry: "CN", wantChina: true},
		{name: "china ipv6 aliyun", ip: "2400:3200::1", wantCountry: "CN", wantChina: true},
		{name: "non china ipv4 google", ip: "8.8.8.8", wantCountry: "US", wantChina: false},
		{name: "non china ipv6 google", ip: "2001:4860:4860::8888", wantCountry: "US", wantChina: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := checker.Lookup(tc.ip)
			require.NoError(t, err)
			require.Equal(t, tc.wantCountry, result.CountryCode)
			require.Equal(t, tc.wantChina, result.IsChina)

			isChina, err := checker.IsChinaIP(tc.ip)
			require.NoError(t, err)
			require.Equal(t, tc.wantChina, isChina)
		})
	}
}

func TestChinaIPCheckerLookupNormalizesHostPort(t *testing.T) {
	checker, err := NewChinaIPChecker(testCountryDBPath(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, checker.Close()) })

	isChina, err := checker.IsChinaIP("114.114.114.114:443")
	require.NoError(t, err)
	require.True(t, isChina)
}

func TestChinaIPCheckerLookupInvalidIP(t *testing.T) {
	checker, err := NewChinaIPChecker(testCountryDBPath(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, checker.Close()) })

	_, err = checker.Lookup("not-an-ip")
	require.Error(t, err)
}

func TestChinaIPCheckerReloadIfChangedNoop(t *testing.T) {
	checker, err := NewChinaIPChecker(testCountryDBPath(t))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, checker.Close()) })

	changed, err := checker.ReloadIfChanged()
	require.NoError(t, err)
	require.False(t, changed)
}

func TestPackageLevelIsChinaIPUsesDefaultResourcePath(t *testing.T) {
	resetDefaultChecker(t)
	packageDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(filepath.Join(packageDir, "..", "..", "..")))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(packageDir))
		resetDefaultChecker(t)
	})

	isChina, err := IsChinaIP("114.114.114.114")
	require.NoError(t, err)
	require.True(t, isChina)
}

func resetDefaultChecker(t *testing.T) {
	t.Helper()
	defaultCheckerMu.Lock()
	checker := defaultChecker
	defaultChecker = nil
	defaultCheckerMu.Unlock()
	if checker != nil {
		require.NoError(t, checker.Close())
	}
}

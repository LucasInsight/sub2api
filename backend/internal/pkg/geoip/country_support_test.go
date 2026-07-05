package geoip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEffectiveCountryCode(t *testing.T) {
	require.Equal(t, "US", EffectiveCountryCode(LookupResult{CountryCode: " us "}))
	require.Equal(t, "AU", EffectiveCountryCode(LookupResult{RegisteredCountryCode: "au"}))
	require.Equal(t, "SG", EffectiveCountryCode(LookupResult{RepresentedCountryCode: "sg"}))
	require.Empty(t, EffectiveCountryCode(LookupResult{}))
}

func TestIsBlockedCountry(t *testing.T) {
	require.True(t, IsBlockedCountry("cn", []string{"US", "CN"}))
	require.True(t, IsBlockedCountry(" CN ", []string{" cn "}))
	require.False(t, IsBlockedCountry("JP", []string{"US", "CN"}))
	require.False(t, IsBlockedCountry("", []string{"CN"}))
}

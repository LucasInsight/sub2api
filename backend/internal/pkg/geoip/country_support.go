package geoip

import "strings"

// EffectiveCountryCode returns the best country code for access policy checks.
func EffectiveCountryCode(result LookupResult) string {
	if result.CountryCode != "" {
		return strings.ToUpper(strings.TrimSpace(result.CountryCode))
	}
	if result.RegisteredCountryCode != "" {
		return strings.ToUpper(strings.TrimSpace(result.RegisteredCountryCode))
	}
	return strings.ToUpper(strings.TrimSpace(result.RepresentedCountryCode))
}

// IsBlockedCountry reports whether countryCode is present in blockedCountryCodes.
func IsBlockedCountry(countryCode string, blockedCountryCodes []string) bool {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if countryCode == "" {
		return false
	}
	for _, blocked := range blockedCountryCodes {
		if countryCode == strings.ToUpper(strings.TrimSpace(blocked)) {
			return true
		}
	}
	return false
}

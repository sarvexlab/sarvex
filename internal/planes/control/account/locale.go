// Copyright 2026 Sarvex Labs
// SPDX-License-Identifier: Apache-2.0

package account

import "context"

// CountryLocale is a country and its selectable IANA timezones, returned by
// GET /account (locale_options) to drive the console's Locale dropdowns —
// country selection filters the timezone list, so the client relies
// entirely on what the server returns rather than validating pairs itself.
type CountryLocale struct {
	Code      string   // ISO 3166-1 alpha-2, e.g. "AU"
	Name      string   // display name, e.g. "Australia"
	TimeZones []string // IANA timezone names available for this country
}

// LocaleProvider supplies the country/timezone reference list. Backed by a
// static in-process list for now (staticLocaleProvider); swap for a
// database-backed implementation later without touching callers — the
// interface is the seam.
type LocaleProvider interface {
	List(ctx context.Context) ([]CountryLocale, error)
}

// staticLocaleProvider serves a curated, hardcoded list of major markets.
// Not the full ISO/IANA country set — extending it (or moving it to a
// database, per the interface's purpose) is a separate task.
type staticLocaleProvider struct{}

// NewStaticLocaleProvider constructs the static, in-process LocaleProvider.
func NewStaticLocaleProvider() LocaleProvider {
	return staticLocaleProvider{}
}

func (staticLocaleProvider) List(_ context.Context) ([]CountryLocale, error) {
	return staticCountryLocales, nil
}

var staticCountryLocales = []CountryLocale{
	{Code: "AU", Name: "Australia", TimeZones: []string{"Australia/Sydney", "Australia/Melbourne", "Australia/Brisbane", "Australia/Perth", "Australia/Adelaide", "Australia/Darwin", "Australia/Hobart"}},
	{Code: "NZ", Name: "New Zealand", TimeZones: []string{"Pacific/Auckland", "Pacific/Chatham"}},
	{Code: "US", Name: "United States", TimeZones: []string{"America/New_York", "America/Chicago", "America/Denver", "America/Los_Angeles", "America/Anchorage", "Pacific/Honolulu"}},
	{Code: "CA", Name: "Canada", TimeZones: []string{"America/St_Johns", "America/Halifax", "America/Toronto", "America/Winnipeg", "America/Edmonton", "America/Vancouver"}},
	{Code: "GB", Name: "United Kingdom", TimeZones: []string{"Europe/London"}},
	{Code: "IE", Name: "Ireland", TimeZones: []string{"Europe/Dublin"}},
	{Code: "DE", Name: "Germany", TimeZones: []string{"Europe/Berlin"}},
	{Code: "FR", Name: "France", TimeZones: []string{"Europe/Paris"}},
	{Code: "ES", Name: "Spain", TimeZones: []string{"Europe/Madrid", "Atlantic/Canary"}},
	{Code: "IT", Name: "Italy", TimeZones: []string{"Europe/Rome"}},
	{Code: "NL", Name: "Netherlands", TimeZones: []string{"Europe/Amsterdam"}},
	{Code: "SE", Name: "Sweden", TimeZones: []string{"Europe/Stockholm"}},
	{Code: "SG", Name: "Singapore", TimeZones: []string{"Asia/Singapore"}},
	{Code: "JP", Name: "Japan", TimeZones: []string{"Asia/Tokyo"}},
	{Code: "IN", Name: "India", TimeZones: []string{"Asia/Kolkata"}},
	{Code: "ZA", Name: "South Africa", TimeZones: []string{"Africa/Johannesburg"}},
	{Code: "BR", Name: "Brazil", TimeZones: []string{"America/Sao_Paulo", "America/Manaus", "America/Fortaleza"}},
	{Code: "MX", Name: "Mexico", TimeZones: []string{"America/Mexico_City", "America/Tijuana", "America/Cancun"}},
	{Code: "AE", Name: "United Arab Emirates", TimeZones: []string{"Asia/Dubai"}},
	{Code: "PH", Name: "Philippines", TimeZones: []string{"Asia/Manila"}},
}

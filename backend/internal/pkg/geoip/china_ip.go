package geoip

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

const (
	DefaultCountryDBPath = "./resources/geoip/GeoLite2-Country.mmdb"
	chinaCountryCode     = "CN"
)

var (
	defaultCheckerMu sync.Mutex
	defaultChecker   *ChinaIPChecker
)

type ChinaIPChecker struct {
	dbPath string

	mu     sync.RWMutex
	reader *maxminddb.Reader
	mtime  time.Time
	size   int64
}

type LookupResult struct {
	IP                     string `json:"ip"`
	CountryCode            string `json:"country_code,omitempty"`
	RegisteredCountryCode  string `json:"registered_country_code,omitempty"`
	RepresentedCountryCode string `json:"represented_country_code,omitempty"`
	IsChina                bool   `json:"is_china"`
}

type countryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	RegisteredCountry struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"registered_country"`
	RepresentedCountry struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"represented_country"`
}

func IsChinaIP(ipText string) (bool, error) {
	checker, err := defaultChinaIPChecker()
	if err != nil {
		return false, err
	}
	return checker.IsChinaIP(ipText)
}

func Lookup(ipText string) (LookupResult, error) {
	checker, err := defaultChinaIPChecker()
	if err != nil {
		return LookupResult{}, err
	}
	return checker.Lookup(ipText)
}

func ReloadDefaultIfChanged() (bool, error) {
	checker, err := defaultChinaIPChecker()
	if err != nil {
		return false, err
	}
	return checker.ReloadIfChanged()
}

func NewChinaIPChecker(dbPath string) (*ChinaIPChecker, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		dbPath = DefaultCountryDBPath
	}
	checker := &ChinaIPChecker{dbPath: dbPath}
	if err := checker.Reload(); err != nil {
		return nil, err
	}
	return checker, nil
}

func defaultChinaIPChecker() (*ChinaIPChecker, error) {
	defaultCheckerMu.Lock()
	defer defaultCheckerMu.Unlock()
	if defaultChecker != nil {
		return defaultChecker, nil
	}
	checker, err := NewChinaIPChecker(DefaultCountryDBPath)
	if err != nil {
		return nil, err
	}
	defaultChecker = checker
	return defaultChecker, nil
}

func (c *ChinaIPChecker) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.reader == nil {
		return nil
	}
	err := c.reader.Close()
	c.reader = nil
	return err
}

func (c *ChinaIPChecker) ReloadIfChanged() (bool, error) {
	if c == nil {
		return false, errors.New("nil ChinaIPChecker")
	}
	st, err := os.Stat(c.dbPath)
	if err != nil {
		return false, fmt.Errorf("stat mmdb %q: %w", c.dbPath, err)
	}

	c.mu.RLock()
	unchanged := st.ModTime().Equal(c.mtime) && st.Size() == c.size
	c.mu.RUnlock()
	if unchanged {
		return false, nil
	}
	return true, c.Reload()
}

func (c *ChinaIPChecker) Reload() error {
	if c == nil {
		return errors.New("nil ChinaIPChecker")
	}
	st, err := os.Stat(c.dbPath)
	if err != nil {
		return fmt.Errorf("stat mmdb %q: %w", c.dbPath, err)
	}
	reader, err := maxminddb.Open(c.dbPath)
	if err != nil {
		return fmt.Errorf("open mmdb %q: %w", c.dbPath, err)
	}

	c.mu.Lock()
	old := c.reader
	c.reader = reader
	c.mtime = st.ModTime()
	c.size = st.Size()
	c.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (c *ChinaIPChecker) IsChinaIP(ipText string) (bool, error) {
	result, err := c.Lookup(ipText)
	if err != nil {
		return false, err
	}
	return result.IsChina, nil
}

func (c *ChinaIPChecker) Lookup(ipText string) (LookupResult, error) {
	if c == nil {
		return LookupResult{}, errors.New("nil ChinaIPChecker")
	}
	ip := parseIP(ipText)
	if ip == nil {
		return LookupResult{}, fmt.Errorf("invalid IP %q", ipText)
	}

	c.mu.RLock()
	reader := c.reader
	defer c.mu.RUnlock()
	if reader == nil {
		return LookupResult{}, errors.New("mmdb reader is not loaded")
	}

	var record countryRecord
	if err := reader.Lookup(ip, &record); err != nil {
		return LookupResult{}, err
	}

	countryCode := strings.ToUpper(record.Country.ISOCode)
	return LookupResult{
		IP:                     ip.String(),
		CountryCode:            countryCode,
		RegisteredCountryCode:  strings.ToUpper(record.RegisteredCountry.ISOCode),
		RepresentedCountryCode: strings.ToUpper(record.RepresentedCountry.ISOCode),
		IsChina:                countryCode == chinaCountryCode,
	}, nil
}

func parseIP(ipText string) net.IP {
	ipText = strings.TrimSpace(ipText)
	if ipText == "" {
		return nil
	}
	if host, _, err := net.SplitHostPort(ipText); err == nil {
		ipText = host
	}
	ip := net.ParseIP(ipText)
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip.To16()
}

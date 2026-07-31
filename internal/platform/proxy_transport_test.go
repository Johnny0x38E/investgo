package platform

import (
	"net/url"
	"testing"

	utls "github.com/refraction-networking/utls"
)

func TestNewChromeSpecDoesNotShareExtensions(t *testing.T) {
	first, err := newChromeSpec()
	if err != nil {
		t.Fatalf("create first Chrome spec: %v", err)
	}
	second, err := newChromeSpec()
	if err != nil {
		t.Fatalf("create second Chrome spec: %v", err)
	}

	firstSNI := findSNIExtension(t, first)
	secondSNI := findSNIExtension(t, second)
	if firstSNI == secondSNI {
		t.Fatal("Chrome specs share an SNI extension")
	}

	firstSNI.ServerName = "api.frankfurter.dev"
	if secondSNI.ServerName != "" {
		t.Fatalf("second Chrome spec inherited SNI %q", secondSNI.ServerName)
	}
}

func TestEnvironmentProxyFuncReadsEnvironmentWhenConfigured(t *testing.T) {
	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
	} {
		t.Setenv(key, "")
	}

	target, err := url.Parse("https://example.com/data")
	if err != nil {
		t.Fatalf("parse target URL: %v", err)
	}
	withoutProxy := environmentProxyFunc()
	if proxyURL, err := withoutProxy(target); err != nil || proxyURL != nil {
		t.Fatalf("unexpected proxy without environment: proxy=%v err=%v", proxyURL, err)
	}

	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
	withProxy := environmentProxyFunc()
	proxyURL, err := withProxy(target)
	if err != nil {
		t.Fatalf("resolve configured proxy: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7897" {
		t.Fatalf("got proxy %v, want http://127.0.0.1:7897", proxyURL)
	}
}

func findSNIExtension(t *testing.T, spec utls.ClientHelloSpec) *utls.SNIExtension {
	t.Helper()
	for _, extension := range spec.Extensions {
		if sni, ok := extension.(*utls.SNIExtension); ok {
			return sni
		}
	}
	t.Fatal("Chrome spec has no SNI extension")
	return nil
}

package platform

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
)

// ProxyTransport wraps a standard http.Transport with a dynamically
// configurable proxy function. All market-data HTTP clients share one
// instance so that changing the proxy setting takes effect globally.
//
// TLS handshakes use utls with a Chrome-compatible ClientHello fingerprint
// so that servers employing JA3/JA4 detection cannot distinguish these
// connections from real Chrome browsers.
type ProxyTransport struct {
	mu   sync.RWMutex
	mode string   // "system" | "custom" | "none"
	url  *url.URL // non-nil only when mode == "custom"

	inner *http.Transport
}

// NewProxyTransport builds a ProxyTransport initialised to the given mode/URL.
// Pass mode "system", "custom", or "none".
func NewProxyTransport(mode, rawURL string) *ProxyTransport {
	pt := &ProxyTransport{mode: mode}
	if mode == "custom" && rawURL != "" {
		pt.url, _ = url.Parse(rawURL)
	}

	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	pt.inner = &http.Transport{
		Proxy:       pt.proxyFunc,
		DialContext: dialer.DialContext,
		// DialTLSContext performs TLS handshakes with a Chrome fingerprint
		// via utls, preventing servers from identifying Go's default TLS
		// ClientHello and returning EOF / connection resets.
		//
		// Note: when an HTTP proxy is configured the transport handles the
		// CONNECT tunnel internally and performs a standard Go TLS handshake
		// on the tunnelled connection — DialTLSContext is only invoked for
		// direct (non-proxied) HTTPS requests.
		DialTLSContext:        chromeTLSDialer(dialer),
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		// ForceAttemptHTTP2 is intentionally omitted: DialTLSContext
		// bypasses Go's built-in HTTP/2 negotiation. HTTP/1.1 is
		// sufficient for every upstream API this app communicates with.
	}
	return pt
}

// tlsHandshakeConcurrency limits how many utls TLS handshakes run at once.
// Each handshake is CPU-intensive (Chrome fingerprint emulation + crypto);
// unbounded concurrent handshakes during a burst of new HTTPS connections
// (e.g. auto-refresh + search firing together) saturate the CPU and on
// macOS starve the WKWebView main thread, freezing the entire window.
// 2 keeps throughput acceptable while leaving CPU headroom for the UI.
const tlsHandshakeConcurrency = 2

// chromeSpec caches the Chrome ClientHello spec with http/1.1-only ALPN so it
// is built once per process instead of on every new TLS connection.
var (
	chromeSpec     utls.ClientHelloSpec
	chromeSpecOnce sync.Once
)

func getChromeSpec() (utls.ClientHelloSpec, error) {
	var specErr error
	chromeSpecOnce.Do(func() {
		spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
		if err != nil {
			specErr = err
			return
		}
		for i, ext := range spec.Extensions {
			if alpn, ok := ext.(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
				spec.Extensions[i] = alpn
				break
			}
		}
		chromeSpec = spec
	})
	return chromeSpec, specErr
}

// tlsHandshakeSem limits concurrent utls handshakes to protect the UI thread.
var tlsHandshakeSem = make(chan struct{}, tlsHandshakeConcurrency)

// chromeTLSDialer returns a DialTLSContext function that dials TCP and then
// performs a TLS handshake using utls with HelloChrome_Auto, which
// automatically selects the latest Chrome ClientHello fingerprint.
func chromeTLSDialer(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		rawConn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		host, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			host = addr
		}

		spec, specErr := getChromeSpec()
		if specErr != nil {
			rawConn.Close()
			return nil, specErr
		}

		tlsConn := utls.UClient(rawConn, &utls.Config{ServerName: host}, utls.HelloCustom)
		if err := tlsConn.ApplyPreset(&spec); err != nil {
			rawConn.Close()
			return nil, err
		}

		// Limit concurrent handshakes so a burst of new connections does not
		// saturate the CPU and freeze the macOS webview main thread.
		select {
		case tlsHandshakeSem <- struct{}{}:
			defer func() { <-tlsHandshakeSem }()
		case <-ctx.Done():
			rawConn.Close()
			return nil, ctx.Err()
		}

		if err := tlsConn.HandshakeContext(ctx); err != nil {
			rawConn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
}

// Update changes the proxy configuration at runtime and closes idle
// connections so that subsequent requests use the new settings.
func (pt *ProxyTransport) Update(mode, rawURL string) {
	pt.mu.Lock()
	pt.mode = mode
	pt.url = nil
	if mode == "custom" && rawURL != "" {
		pt.url, _ = url.Parse(rawURL)
	}
	pt.mu.Unlock()
	pt.inner.CloseIdleConnections()
}

// RoundTrip implements http.RoundTripper.
func (pt *ProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return pt.inner.RoundTrip(req)
}

// proxyFunc is the dynamic proxy resolution callback used by the inner transport.
func (pt *ProxyTransport) proxyFunc(req *http.Request) (*url.URL, error) {
	pt.mu.RLock()
	mode := pt.mode
	u := pt.url
	pt.mu.RUnlock()

	switch mode {
	case "none":
		return nil, nil
	case "custom":
		if u != nil {
			return u, nil
		}
		return nil, nil
	default: // "system"
		return http.ProxyFromEnvironment(req)
	}
}

// NewHTTPClient returns an *http.Client that uses the given ProxyTransport.
func NewHTTPClient(pt *ProxyTransport) *http.Client {
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: pt,
	}
}

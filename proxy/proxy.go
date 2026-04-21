package proxy

import (
	"github.com/snowmerak/llmrouter/config"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewOllamaProxy creates a new reverse proxy with the ReloadableTransport
func NewOllamaProxy(cfg *config.Config) (*httputil.ReverseProxy, *ReloadableTransport) {
	customTransport := NewReloadableTransport(cfg, http.DefaultTransport)

	// We use a dummy target for the ReverseProxy because actual routing
	// based on failover logic is handled within our custom RoundTripper.
	dummyTarget, _ := url.Parse("http://dummy-target")

	proxy := httputil.NewSingleHostReverseProxy(dummyTarget)

	// Override the Director to preserve the original path and query
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Our custom transport will overwrite the Host and Scheme later
		// But we keep this simple here.
	}

	proxy.Transport = customTransport

	return proxy, customTransport
}

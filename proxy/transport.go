package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/snowmerak/llmrouter/adapter/anthropic"
	"github.com/snowmerak/llmrouter/adapter/openai"
	"github.com/snowmerak/llmrouter/config"
	"github.com/snowmerak/llmrouter/schema"

	"github.com/sony/gobreaker"
)

type MultiTransport struct {
	baseTransport http.RoundTripper
	destinations  []*destinationNode
	cfg           *config.Config
}

type destinationNode struct {
	url            *url.URL
	breaker        *gobreaker.CircuitBreaker
	weight         int
	tags           []string
	targetModel    string
	protocol       string
	apiKey         string
	isAlive        atomic.Bool
	activeRequests atomic.Int32
}

type trackingReadCloser struct {
	io.ReadCloser
	onClose func()
	closed  atomic.Bool
}

func (t *trackingReadCloser) Close() error {
	if t.closed.CompareAndSwap(false, true) {
		t.onClose()
	}
	if t.ReadCloser != nil {
		return t.ReadCloser.Close()
	}
	return nil
}

type streamParser func(line []byte) (*schema.ChatStreamChunk, error)

type unifiedStreamRewriter struct {
	originalBody  io.ReadCloser
	reader        *bufio.Reader
	buf           bytes.Buffer
	err           error
	targetModel   string
	originalModel string
	sentDone      bool
	parser        streamParser
}

func (r *unifiedStreamRewriter) Read(p []byte) (n int, err error) {
	if r.buf.Len() > 0 {
		return r.buf.Read(p)
	}
	if r.err != nil {
		return 0, r.err
	}

	line, readErr := r.reader.ReadBytes('\n')
	if len(line) > 0 {
		chunk, parseErr := r.parser(line)

		if parseErr == nil && chunk != nil {
			if chunk.Model == r.targetModel || chunk.Model == "" {
				chunk.Model = r.originalModel
			}
			formatted, formatErr := openai.FormatStreamChunk(chunk)
			if formatErr == nil {
				r.buf.Write(formatted)
			}
		} else if parseErr == nil && chunk == nil {
			if len(bytes.TrimSpace(line)) == 0 {
				r.buf.Write([]byte("\n"))
			}
		}
	}
	if readErr != nil {
		if readErr == io.EOF && !r.sentDone {
			r.buf.Write([]byte("data: [DONE]\n\n"))
			r.sentDone = true
		}
		r.err = readErr
	}

	if r.buf.Len() > 0 {
		return r.buf.Read(p)
	}
	return 0, r.err
}

func (r *unifiedStreamRewriter) Close() error {
	return r.originalBody.Close()
}

// NewMultiTransport creates a custom HTTP transport with fallback and circuit breaker logic
func NewMultiTransport(ctx context.Context, cfg *config.Config, baseTransport http.RoundTripper) *MultiTransport {
	if baseTransport == nil {
		baseTransport = http.DefaultTransport
	}

	nodes := make([]*destinationNode, 0, len(cfg.Destinations))
	for _, dest := range cfg.Destinations {
		u, err := url.Parse(dest.URL)
		if err != nil {
			log.Fatalf("Invalid destination URL %s: %v", dest.URL, err)
		}

		cbSettings := gobreaker.Settings{
			Name:        "CB-" + u.Host,
			MaxRequests: cfg.CircuitBreaker.MaxRequests,
			Interval:    time.Duration(cfg.CircuitBreaker.IntervalSecs) * time.Second,
			Timeout:     time.Duration(cfg.CircuitBreaker.TimeoutSecs) * time.Second,
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= 1
			},
			OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
				log.Printf("[CircuitBreaker] %s transitioned from %s to %s", name, from.String(), to.String())
			},
		}

		w := dest.Weight
		if w < 0 {
			w = 0
		}

		proto := dest.Protocol
		if proto == "" {
			proto = "openai" // default fallback
		}

		node := &destinationNode{
			url:         u,
			breaker:     gobreaker.NewCircuitBreaker(cbSettings),
			weight:      w,
			tags:        dest.Tags,
			targetModel: dest.TargetModel,
			protocol:    proto,
			apiKey:      dest.ApiKey,
		}
		// Default to true so we don't drop requests before first ping returns
		node.isAlive.Store(true)

		nodes = append(nodes, node)

		if cfg.HealthCheck.Enabled {
			go startHealthCheckLoop(ctx, node, cfg.HealthCheck)
		}
	}

	return &MultiTransport{
		baseTransport: baseTransport,
		destinations:  nodes,
		cfg:           cfg,
	}
}

func (t *MultiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so it can be re-read on retries.
	// httputil.ReverseProxy doesn't buffer bodies automatically for retries.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}

	isMetadataRoute := strings.HasPrefix(req.URL.Path, "/api/tags") || 
						strings.HasPrefix(req.URL.Path, "/api/show") || 
						strings.HasPrefix(req.URL.Path, "/api/version") || 
						strings.HasPrefix(req.URL.Path, "/api/ps")

	var requestedModel string
	var universalReq *schema.ChatRequest
	if len(bodyBytes) > 0 {
		if req, err := openai.ToUniversalRequest(bodyBytes); err == nil && req.Model != "" {
			universalReq = req
			requestedModel = req.Model
		} else {
			var payload map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &payload); err == nil {
				if m, ok := payload["model"].(string); ok {
					requestedModel = m
				}
			}
		}
	}

	var requiredTag string
	if !isMetadataRoute && requestedModel != "" {
		requiredTag = requestedModel // Default to requested model as the tag
		if t.cfg != nil && t.cfg.ModelRouting != nil {
			if mappedTag, ok := t.cfg.ModelRouting[requestedModel]; ok {
				requiredTag = mappedTag // Override if explicitly mapped
			}
		}
	}

	var lastErr error

	// Filter nodes to only alive ones
	var remainingNodes []*destinationNode
	isOllamaRoute := strings.HasPrefix(req.URL.Path, "/api/")

	for _, n := range t.destinations {
		if n.isAlive.Load() {
			if isMetadataRoute {
				// Metadata requests only go to nodes tagged as "ollama"
				hasOllamaTag := false
				for _, tag := range n.tags {
					if tag == "ollama" {
						hasOllamaTag = true
						break
					}
				}
				if !hasOllamaTag {
					continue
				}
			} else {
				// Base routing constraints
				if isOllamaRoute {
					hasOllamaTag := false
					for _, tag := range n.tags {
						if tag == "ollama" {
							hasOllamaTag = true
							break
						}
					}
					if !hasOllamaTag {
						continue
					}
				}
				
				// Group routing constraints
				if requiredTag != "" {
					hasRequiredTag := false
					for _, tag := range n.tags {
						if tag == requiredTag {
							hasRequiredTag = true
							break
						}
					}
					if !hasRequiredTag {
						continue
					}
				}
			}
			remainingNodes = append(remainingNodes, n)
		}
	}

	// Fallback to trying all nodes blindly if ping logic deemed them ALL dead
	// but respect 502 logic if there are no available tagged nodes.
	if len(remainingNodes) == 0 && len(t.destinations) > 0 {
		if isOllamaRoute || requiredTag != "" {
			return make502Response(), nil
		}
		remainingNodes = make([]*destinationNode, len(t.destinations))
		copy(remainingNodes, t.destinations)
	}

	for len(remainingNodes) > 0 {
		idx := selectLeastUtilized(remainingNodes, isMetadataRoute)
		if idx == -1 {
			// Fast fail: all alive nodes are at max capacity
			return make429Response(), nil
		}
		node := remainingNodes[idx]

		// Remove the chosen node from remainingNodes
		remainingNodes = append(remainingNodes[:idx], remainingNodes[idx+1:]...)

		// Clone request for this attempt
		attemptReq := req.Clone(req.Context())
		
		var originalModel string
		attemptBodyBytes := bodyBytes

		if node.protocol == "openai" && universalReq != nil && node.targetModel != "" {
			originalModel = universalReq.Model
			clonedReq := *universalReq // shallow copy
			clonedReq.Model = node.targetModel

			if newBody, err := openai.FromUniversalRequest(&clonedReq); err == nil {
				logLimit := len(newBody)
				if logLimit > 150 {
					logLimit = 150
				}
				log.Printf("[Proxy Rewrite Adapter] Changed request model '%s' -> '%s' (New Length: %d, Payload Front: %s...)", originalModel, node.targetModel, len(newBody), string(newBody[:logLimit]))
				attemptBodyBytes = newBody
				attemptReq.ContentLength = int64(len(attemptBodyBytes))
				attemptReq.Header.Set("Content-Length", strconv.Itoa(len(attemptBodyBytes)))
			} else {
				log.Printf("[Proxy Error] Failed to marshal updated payload via adapter: %v", err)
			}
		} else if node.protocol == "anthropic" && universalReq != nil {
			clonedReq := *universalReq
			if node.targetModel != "" {
				clonedReq.Model = node.targetModel
			}
			if newBody, err := anthropic.FromUniversalRequest(&clonedReq); err == nil {
				attemptBodyBytes = newBody
				attemptReq.ContentLength = int64(len(attemptBodyBytes))
				attemptReq.Header.Set("Content-Length", strconv.Itoa(len(attemptBodyBytes)))
			}
			if node.apiKey != "" {
				attemptReq.Header.Set("x-api-key", node.apiKey)
			}
			attemptReq.Header.Set("anthropic-version", "2023-06-01")
			attemptReq.URL.Path = "/v1/messages"
		} else if bodyBytes != nil && node.targetModel != "" {
			var payload map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &payload); err == nil {
				if m, ok := payload["model"].(string); ok {
					originalModel = m
					payload["model"] = node.targetModel
					if newBody, err := json.Marshal(payload); err == nil {
						logLimit := len(newBody)
						if logLimit > 150 {
							logLimit = 150
						}
						log.Printf("[Proxy Rewrite Map] Changed request model '%s' -> '%s' (New Length: %d, Payload Front: %s...)", originalModel, node.targetModel, len(newBody), string(newBody[:logLimit]))
						attemptBodyBytes = newBody
						attemptReq.ContentLength = int64(len(attemptBodyBytes))
						attemptReq.Header.Set("Content-Length", strconv.Itoa(len(attemptBodyBytes)))
					} else {
						log.Printf("[Proxy Error] Failed to marshal updated payload: %v", err)
					}
				}
			} else {
				log.Printf("[Proxy Warning] Could not unmarshal request JSON for model replace: %v", err)
			}
		}

		if attemptBodyBytes != nil {
			attemptReq.Body = io.NopCloser(bytes.NewReader(attemptBodyBytes))
			attemptReq.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(attemptBodyBytes)), nil
			}
		}

		// Disable upstream gzip so our response stream replacement (body scanning) works
		attemptReq.Header.Del("Accept-Encoding")

		// Rewrite the target URL
		attemptReq.URL.Scheme = node.url.Scheme
		attemptReq.URL.Host = node.url.Host
		attemptReq.Host = node.url.Host

		// Use Circuit Breaker Execute
		res, err := node.breaker.Execute(func() (interface{}, error) {
			node.activeRequests.Add(1)

			start := time.Now()
			log.Printf("[Proxy Out] Starting request to %s (Path: %s)", node.url.Host, attemptReq.URL.Path)

			resp, reqErr := t.baseTransport.RoundTrip(attemptReq)
			
			elapsed := time.Since(start)

			if reqErr != nil {
				log.Printf("[Proxy Error] Request to %s failed after %v: %v", node.url.Host, elapsed, reqErr)
				node.activeRequests.Add(-1)
				return resp, reqErr
			}

			log.Printf("[Proxy Success] Received status %d from %s after %v", resp.StatusCode, node.url.Host, elapsed)

			// Consider 502, 503, 504 as failures that should trip the breaker
			if resp != nil && (resp.StatusCode == http.StatusBadGateway ||
				resp.StatusCode == http.StatusServiceUnavailable ||
				resp.StatusCode == http.StatusGatewayTimeout) {
				// We return error to trip the breaker, wrapper handles response
				node.activeRequests.Add(-1)
				return resp, http.ErrServerClosed // fake error to register failure
			}

			if resp != nil && resp.Body != nil {
				isStreamResp := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") ||
					req.Header.Get("Accept") == "text/event-stream" || attemptReq.Header.Get("Accept") == "text/event-stream"

				var finalBody io.ReadCloser = resp.Body

				if isStreamResp {
					var parser streamParser

					if node.protocol == "anthropic" {
						log.Printf("[Proxy Rewrite Adapter] Activating Anthropic unified stream translator ('%s' -> '%s')", node.targetModel, originalModel)
						var currentID, currentModel string
						parser = func(line []byte) (*schema.ChatStreamChunk, error) {
							chunk, nextID, nextModel, err := anthropic.ParseStreamChunk(line, currentID, currentModel)
							currentID = nextID
							currentModel = nextModel
							return chunk, err
						}
					} else if node.protocol == "openai" && originalModel != "" && node.targetModel != "" && originalModel != node.targetModel {
						log.Printf("[Proxy Rewrite Adapter] Activating OpenAI unified stream rewriter ('%s' -> '%s')", node.targetModel, originalModel)
						parser = func(line []byte) (*schema.ChatStreamChunk, error) {
							return openai.ParseStreamChunk(line)
						}
					}

					if parser != nil {
						finalBody = &unifiedStreamRewriter{
							originalBody:  resp.Body,
							reader:        bufio.NewReader(resp.Body),
							targetModel:   node.targetModel,
							originalModel: originalModel,
							parser:        parser,
						}
					}
				} else {
					if originalModel != "" && node.targetModel != "" {
						bodyBytes, err := io.ReadAll(resp.Body)
						resp.Body.Close()

						if err == nil {
							if node.protocol == "anthropic" {
								log.Printf("[Proxy Rewrite Adapter] Activating Anthropic non-streaming unified translator ('%s' -> '%s')", node.targetModel, originalModel)
								universalResp, err := anthropic.ToUniversalResponse(bodyBytes)
								if err == nil && universalResp != nil {
									if universalResp.Model == node.targetModel {
										universalResp.Model = originalModel
									}
									if newBytes, err := json.Marshal(universalResp); err == nil {
										finalBody = io.NopCloser(bytes.NewReader(newBytes))
										resp.ContentLength = int64(len(newBytes))
										resp.Header.Set("Content-Length", strconv.Itoa(len(newBytes)))
									} else {
										finalBody = io.NopCloser(bytes.NewReader(bodyBytes))
									}
								} else {
									finalBody = io.NopCloser(bytes.NewReader(bodyBytes))
								}
							} else if node.protocol == "openai" {
								log.Printf("[Proxy Rewrite Adapter] Activating OpenAI non-streaming fast rewriter ('%s' -> '%s')", node.targetModel, originalModel)
								replaced := bytes.Replace(bodyBytes, []byte(`"model":"`+node.targetModel+`"`), []byte(`"model":"`+originalModel+`"`), 1)
								replaced = bytes.Replace(replaced, []byte(`"model": "`+node.targetModel+`"`), []byte(`"model": "`+originalModel+`"`), 1)
								finalBody = io.NopCloser(bytes.NewReader(replaced))
								resp.ContentLength = int64(len(replaced))
								resp.Header.Set("Content-Length", strconv.Itoa(len(replaced)))
							}
						} else {
							finalBody = io.NopCloser(bytes.NewReader(bodyBytes))
						}
					}
				}

				resp.Body = &trackingReadCloser{
					ReadCloser: finalBody,
					onClose: func() {
						node.activeRequests.Add(-1)
					},
				}
			} else {
				node.activeRequests.Add(-1)
			}
			return resp, nil
		})

		// Unpack response
		if err != nil {
			lastErr = err
			if res != nil {
				if resp, ok := res.(*http.Response); ok && resp != nil && resp.Body != nil {
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
				}
			}
			log.Printf("Destination %s failed: %v, trying next...", node.url.Host, err)
			continue // Try next destination
		}

		if res == nil {
			log.Printf("Destination %s returned nil response, trying next...", node.url.Host)
			continue
		}
		resp, ok := res.(*http.Response)
		if !ok || resp == nil {
			log.Printf("Destination %s returned invalid response, trying next...", node.url.Host)
			continue
		}

		log.Printf("Successfully routed to %s (Load: %d/%d)", node.url.Host, node.activeRequests.Load(), node.weight)
		return resp, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return make429Response(), nil
}

func selectLeastUtilized(nodes []*destinationNode, bypassWeight bool) int {
	if len(nodes) == 0 {
		return -1
	}

	bestIdx := -1
	minRatio := 1000000.0

	for i, n := range nodes {
		active := n.activeRequests.Load()

		if !bypassWeight {
			if active >= int32(n.weight) {
				continue // Skip full nodes
			}
			ratio := float64(active) / float64(n.weight)
			if bestIdx == -1 || ratio < minRatio {
				minRatio = ratio
				bestIdx = i
			}
		} else {
			// Bypass weight limit for metadata routes. Load balance purely based on active requests
			ratio := float64(active)
			if bestIdx == -1 || ratio < minRatio {
				minRatio = ratio
				bestIdx = i
			}
		}
	}
	return bestIdx
}

func make429Response() *http.Response {
	bodyMsg := []byte("429 Too Many Requests: All Ollama servers are at maximum capacity.")
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Status:     "429 Too Many Requests",
		Body:       io.NopCloser(bytes.NewReader(bodyMsg)),
		Header:     make(http.Header),
	}
}

func make502Response() *http.Response {
	bodyMsg := []byte("502 Bad Gateway: No available targets for Ollama API route.")
	return &http.Response{
		StatusCode: http.StatusBadGateway,
		Status:     "502 Bad Gateway",
		Body:       io.NopCloser(bytes.NewReader(bodyMsg)),
		Header:     make(http.Header),
	}
}

func startHealthCheckLoop(ctx context.Context, node *destinationNode, cfg config.HealthCheck) {
	ticker := time.NewTicker(time.Duration(cfg.IntervalSecs) * time.Second)
	defer ticker.Stop()

	// Perform an initial ping immediately
	pingNode(ctx, node, cfg)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingNode(ctx, node, cfg)
		}
	}
}

func pingNode(ctx context.Context, node *destinationNode, cfg config.HealthCheck) {
	pingURL := *node.url
	pingURL.Path = cfg.PingPath

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pingURL.String(), nil)
	if err != nil {
		return
	}

	client := &http.Client{
		Timeout: time.Duration(cfg.TimeoutSecs) * time.Second,
	}

	resp, err := client.Do(req)
	aliveNow := err == nil && resp != nil && resp.StatusCode < 500

	if resp != nil && resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	wasAlive := node.isAlive.Load()
	if wasAlive != aliveNow {
		node.isAlive.Store(aliveNow)
		if aliveNow {
			log.Printf("[Ping] Node %s is alive again", node.url.Host)
		} else {
			log.Printf("[Ping] Node %s marked as dead", node.url.Host)
		}
	}
}

type ReloadableTransport struct {
	mu            sync.RWMutex
	inner         *MultiTransport
	baseTransport http.RoundTripper
	cancelFn      context.CancelFunc
}

func NewReloadableTransport(cfg *config.Config, base http.RoundTripper) *ReloadableTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ReloadableTransport{
		inner:         NewMultiTransport(ctx, cfg, base),
		baseTransport: base,
		cancelFn:      cancel,
	}
}

func (t *ReloadableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	active := t.inner
	t.mu.RUnlock()

	return active.RoundTrip(req)
}

func (t *ReloadableTransport) Update(cfg *config.Config) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Cancel old transport's health check loops
	if t.cancelFn != nil {
		t.cancelFn()
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.inner = NewMultiTransport(ctx, cfg, t.baseTransport)
	t.cancelFn = cancel

	log.Printf("Proxy transport successfully reloaded with new configuration.")
}

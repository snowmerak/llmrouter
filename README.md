# LLM Router

LLM Router is a high-performance, reverse-proxy based API Gateway designed to unify multiple LLM protocols. It provides a seamless **M x N bidirectional routing pipeline**, allowing clients to use their preferred SDK (OpenAI or Anthropic) while routing requests to any supported backend (OpenAI, Anthropic, or Vertex AI).

## 🚀 Key Features

*   **M x N Protocol Translation:** Clients can send requests using either OpenAI (`/v1/chat/completions`) or Anthropic (`/v1/messages`) formats. The router intercepts the HTTP stream and transparently translates the payload and Server-Sent Events (SSE) to match the target backend.
*   **Universal Tool Calling (Function Calling):** Fully supports bidirectional, cross-provider tool calling. You can seamlessly route OpenAI `tool_calls` to an Anthropic backend, or Anthropic `tool_use` to a Vertex AI (Gemini) backend. The router handles all JSON schema mapping, argument serialization, and streaming delta restructuring automatically.
*   **Reverse Proxy Architecture:** Instead of using heavy provider SDKs, the router operates at the HTTP layer using `httputil.ReverseProxy`. It performs zero-copy stream interception, resulting in ultra-low latency and minimal memory footprint.
*   **Zero-config Vertex AI Auth:** Automatically utilizes Google Application Default Credentials (ADC). No need to hardcode JSON keys; just run `gcloud auth application-default login` on the host machine.
*   **Dynamic Model Routing:** Maps client-requested model names (e.g., `light`, `super`) to specific backend node models (e.g., `gemma-4-26b...`, `minimax-m2.7...`).
*   **Resiliency:** Features built-in Circuit Breakers (via `gobreaker`), background health-checks, round-robin load balancing, and hot-reloadable configurations without dropping active connections.

## ⚙️ Supported Protocols

| Protocol Identifier | Supported as Frontend (Client) | Supported as Backend (Target) |
| :------------------ | :----------------------------: | :---------------------------: |
| `openai`            |   ✅ (`/v1/chat/completions`)   |               ✅               |
| `anthropic`         |       ✅ (`/v1/messages`)       |               ✅               |
| `vertexai`          |      ❌ (Not Recommended)       |         ✅ (REST API)          |

## 🛠️ Getting Started

### 1. Installation

```bash
# Clone the repository and download dependencies
go mod tidy

# If using Vertex AI, ensure Google ADC is configured on your system
gcloud auth application-default login
```

### 2. Configuration (`config.yaml`)

Create a `config.yaml` file in the root directory.

```yaml
server:
  port: 11656

destinations:
  - url: "http://m4max128:1234"
    protocol: "openai"
    weight: 1
    target_model: "qwen3.6-35b-a3b"
    tags: ["super"]

  - url: "http://m4max128:1234"
    protocol: "anthropic"
    weight: 1
    target_model: "gemma-4-26b-a4b-it-uncensored-max"
    tags: ["light"]

  - url: "http://m4max128:1234"
    protocol: "openai"
    weight: 1
    target_model: "text-embedding-qwen3-embedding-0.6b"
    tags: ["embedding"]

  - url: "https://us-central1-aiplatform.googleapis.com/v1/projects/YOUR_PROJECT_ID/locations/us-central1"
    protocol: "vertexai"
    weight: 1
    target_model: "gemini-2.5-pro"
    tags: ["gemini"]

health_check:
  enabled: true
  interval_secs: 10
  timeout_secs: 3
  ping_path: "/"

circuit_breaker:
  max_requests: 3
  interval_secs: 600
  timeout_secs: 300
```

> **💡 Environment Variables:** You can dynamically inject OS environment variables anywhere in the `config.yaml` using the `{{env:VAR_NAME}}` syntax. This is particularly useful for keeping secrets like `api_key` or `url` out of source control.

### 3. Running the Router

```bash
go run .
```
The router will hot-reload automatically if `config.yaml` is modified during runtime.

### 4. Client Usage

You can use the official OpenAI or Anthropic SDKs. Simply change the base URL to point to the router and specify the `tag` as the model name.

**Using OpenAI Client (Python Example):**
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:11656/v1",
    api_key="not-needed"
)

# Requesting 'gemini' tag will seamlessly route to the Vertex AI backend!
response = client.chat.completions.create(
    model="gemini", 
    messages=[{"role": "user", "content": "What is the weather in San Francisco?"}],
    tools=[...], # Tool calling is fully supported!
    stream=True
)

for chunk in response:
    print(chunk.choices[0].delta.content or "", end="")
```

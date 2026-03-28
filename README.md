# cf-llm-proxy

Lightweight reverse proxy that injects Cloudflare Zero Trust authentication headers for OpenAI-compatible LLM endpoints. Built for use with OpenCode + llama-swap.

## Features

- **SSE streaming support** — real-time token streaming, no buffering
- **Zero dependencies** — pure Go standard library
- **Cross-platform** — builds for macOS (ARM/Intel), Linux, Windows
- **Minimal footprint** — single binary, ~5MB

## Quick Start

```bash
export CF_ACCESS_CLIENT_ID="your-client-id"
export CF_ACCESS_CLIENT_SECRET="your-client-secret"

# Build and run
make build
./cf-llm-proxy

# Or run directly
go run ./cmd/proxy
```

The proxy listens on `http://127.0.0.1:8900` by default and forwards to `llm.sark-ai.org`.

## Configuration

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--port` | `CF_PROXY_PORT` | `8900` | Local listen port |
| `--addr` | - | `127.0.0.1` | Listen address |
| `--target` | `CF_TARGET_HOST` | `llm.sark-ai.org` | Upstream LLM host |

Cloudflare credentials (required):
- `CF_ACCESS_CLIENT_ID` — Service token Client ID
- `CF_ACCESS_CLIENT_SECRET` — Service token Client Secret

## Use with OpenCode

Point OpenCode to the proxy in `opencode.json`:

```json
{
  "provider": {
    "llama-swap": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "llama-swap",
      "options": {
        "baseURL": "http://127.0.0.1:8900/v1"
      },
      "models": {
        "[model_ref]": {
          "name": "[Model Name]"
        }
      }
    }
  }
}
```

## Cross-platform Build

```bash
make all          # Build for all platforms
make darwin-arm64 # macOS Apple Silicon only
make linux-amd64  # Linux only
make install      # Build and install to /usr/local/bin
```

## Health Check

```bash
curl http://127.0.0.1:8900/health
```

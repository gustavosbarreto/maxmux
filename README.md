# maxmux

Lightweight gateway that lets you share a single Claude Code Max subscription across multiple Claude Code instances using virtual keys.

maxmux receives requests with a virtual key, validates it, and forwards to Anthropic injecting the real OAuth token from the subscription.

## Setup

### 1. Get the OAuth token

```bash
claude setup-token
```

Paste the generated token (format `sk-ant-oat01-...`) into `config.yaml`.

> **Note:** the token lasts ~1 year. When it expires, run `claude setup-token` again and update `config.yaml`.

### 2. Configure

```yaml
# config.yaml
port: 4000
upstream: https://api.anthropic.com
oauth_token: sk-ant-oat01-YOUR-TOKEN-HERE
virtual_keys:
  - sk-proxy-key-1
  - sk-proxy-key-2
```

Each virtual key is a credential that a Claude Code instance uses to authenticate with the gateway.

### 3. Run

**From source:**

```bash
go build -o maxmux .
./maxmux -config config.yaml
```

**Docker:**

```bash
docker run -d \
  -p 4000:4000 \
  -v $(pwd)/config.yaml:/config.yaml \
  gustavosbarreto/maxmux -config /config.yaml
```

**Kubernetes:**

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: maxmux
stringData:
  config.yaml: |
    port: 4000
    upstream: https://api.anthropic.com
    oauth_token: sk-ant-oat01-YOUR-TOKEN-HERE
    virtual_keys:
      - sk-proxy-key-1
      - sk-proxy-key-2
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: maxmux
spec:
  replicas: 1
  selector:
    matchLabels:
      app: maxmux
  template:
    metadata:
      labels:
        app: maxmux
    spec:
      containers:
        - name: maxmux
          image: gustavosbarreto/maxmux:latest
          args: ["-config", "/config/config.yaml"]
          ports:
            - containerPort: 4000
          volumeMounts:
            - name: config
              mountPath: /config
      volumes:
        - name: config
          secret:
            secretName: maxmux
---
apiVersion: v1
kind: Service
metadata:
  name: maxmux
spec:
  selector:
    app: maxmux
  ports:
    - port: 4000
      targetPort: 4000
```

### 4. Connect Claude Code

On each Claude Code instance:

```bash
ANTHROPIC_BASE_URL=http://localhost:4000 ANTHROPIC_AUTH_TOKEN=sk-proxy-key-1 claude
```

- `ANTHROPIC_AUTH_TOKEN` skips the login screen and sends the virtual key as `Authorization: Bearer`
- maxmux validates it, swaps it for the real OAuth token, and forwards to Anthropic

For a second instance, use `sk-proxy-key-2`.

## Flow

```
Claude Code                         maxmux                          Anthropic
    │                                 │                                │
    │  Authorization: Bearer          │                                │
    │  sk-proxy-key-1                 │                                │
    ├────────────────────────────────►│                                │
    │                                 │  validate virtual key          │
    │                                 │  swap with OAuth token         │
    │                                 │  add oauth headers             │
    │                                 │                                │
    │                                 │  Authorization: Bearer         │
    │                                 │  sk-ant-oat01-...              │
    │                                 │  anthropic-beta: ...,oauth-... │
    │                                 ├───────────────────────────────►│
    │                                 │                                │
    │                                 │◄───────────────────────────────┤
    │◄────────────────────────────────┤  response (streaming)          │
```

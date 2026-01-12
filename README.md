# Prometheus DingTalk Hook

A lightweight Go webhook service:

- Receives Prometheus Alertmanager webhook JSON.
- Renders a message with Go `text/template`.
- Forwards the message to DingTalk group robots (supports signing and mentions).

## Features

- Alertmanager webhook endpoint (default: `POST /alert`)
- Multiple DingTalk robots
- Routing: `channels + routes` (match by receiver/status/labels)
- Mentions: `@all` / `@mobile` / `@userId` (auto appends `@...` tokens in message body)
- Optional token auth (`Authorization: Bearer <token>` or `X-Token: <token>`)
- Safe hot reload (`POST /-/reload`, optional polling)
- Optional Admin UI (`/admin/`) for managing templates and testing messages

## Quick Start

1) Create config:

```bash
cp config.example.yml config.yml
```

2) Edit `config.yml`:

- Set `dingtalk.robots[0].webhook`.
- Ensure `dingtalk.channels` contains a channel named `"default"` with at least one robot.

3) Run:

```bash
go run ./cmd/prometheus-dingtalk-hook -config config.yml
```

## Template

The binary ships with an embedded `default` template.

To add or override templates, configure `template.dir` to a directory containing `*.tmpl`.

```yaml
template:
  dir: "templates"
```

Notes:

- If `template.dir` is empty, the embedded default template is used.
- If `template.dir` does not exist (e.g. not mounted in Docker), the app falls back to the embedded default template.
- `channels[].template` selects a template by name (filename without `.tmpl`), e.g. `default` for `default.tmpl`.

## Markdown Title

For DingTalk `markdown` messages, `dingtalk.robots[].title` controls the `markdown.title` field.

- If `title` is empty, it defaults to Alertmanager `summary`:
  - `commonAnnotations.summary`, then
  - the first alert's `annotations.summary`, then
  - `alertname`, then
  - `"Alertmanager"`.

## Alertmanager Receiver Example

```yaml
receivers:
  - name: ops-team
    webhook_configs:
      - url: "http://prometheus-dingtalk-hook:8080/alert"
        send_resolved: true
```

## Docker

Example:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/config.yml:/app/config.yml:ro" \
  ghcr.io/your-org/prometheus-dingtalk-hook:latest \
  -config /app/config.yml
```

Optional templates directory:

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/config.yml:/app/config.yml:ro" \
  -v "$PWD/templates:/data/templates:ro" \
  ghcr.io/your-org/prometheus-dingtalk-hook:latest \
  -config /app/config.yml
```

And in `config.yml`:

```yaml
template:
  dir: "/data/templates"
```

## Admin UI (Optional)

Enable:

```yaml
admin:
  enabled: true
  path_prefix: "/admin"
  basic_auth:
    username: "admin"
    password: "change-me"
```

## Security Notes

- Never commit `access_token`, `secret`, or `auth.token` to a public repository.
- Run behind internal networking or enable token auth.

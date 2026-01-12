# Prometheus DingTalk Hook

一个轻量的 Go Webhook 服务：

- 接收 Prometheus Alertmanager Webhook（JSON）
- 使用 Go `text/template` 渲染消息
- 转发到钉钉群机器人（支持加签与 @）

## 功能

- Alertmanager Webhook 接收（默认：`POST /alert`）
- 多钉钉机器人配置
- 路由：`channels + routes`（按 receiver/status/labels 匹配）
- @：`@all` / `@手机号` / `@userId`（消息末尾自动追加 `@...`）
- 可选 token 鉴权（`Authorization: Bearer <token>` 或 `X-Token: <token>`）
- 安全热加载（`POST /-/reload`，可选轮询）
- 管理 UI：`/admin/`（模板管理/预览、测试发送、配置导入导出）

## 快速开始

1) 创建配置：

```bash
cp config.example.yml config.yml
```

2) 编辑 `config.yml`：

- 设置 `dingtalk.robots[0].webhook`
- 确保 `dingtalk.channels` 中包含 `name: "default"` 且绑定至少一个机器人

3) 运行：

```bash
go run ./cmd/prometheus-dingtalk-hook -config config.yml
```

## 模板

二进制内置 `default` 模板。

自定义模板仅支持 `template.dir`：加载目录下的 `*.tmpl`（模板名为文件名去掉 `.tmpl`）。

```yaml
template:
  dir: "templates"
```

说明：

- `template.dir` 留空：使用内置 default 模板
- `template.dir` 目录不存在（例如 Docker 未挂载）：自动回退使用内置 default 模板
- `channels[].template` 选择模板名，例如 `default` 对应 `default.tmpl`

## Markdown 标题

当机器人 `msg_type: "markdown"` 时，`dingtalk.robots[].title` 对应钉钉 `markdown.title`。

- `title` 留空：默认使用 Alertmanager 的 `summary`（优先 `commonAnnotations.summary`，否则取第一条 alert 的 `annotations.summary`，再回退 `alertname`，最后为 `"Alertmanager"`）

## Alertmanager 配置示例

```yaml
receivers:
  - name: ops-team
    webhook_configs:
      - url: "http://prometheus-dingtalk-hook:8080/alert"
        send_resolved: true
```

## Docker

基础示例：

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/config.yml:/app/config.yml:ro" \
  ghcr.io/your-org/prometheus-dingtalk-hook:latest \
  -config /app/config.yml
```

可选：挂载模板目录：

```bash
docker run --rm -p 8080:8080 \
  -v "$PWD/config.yml:/app/config.yml:ro" \
  -v "$PWD/templates:/data/templates:ro" \
  ghcr.io/your-org/prometheus-dingtalk-hook:latest \
  -config /app/config.yml
```

并在 `config.yml` 中配置：

```yaml
template:
  dir: "/data/templates"
```

## 一键安装（Linux + systemd）

将自动下载最新 Release，解压后：
- 二进制安装到 `/usr/share/bin/prometheus-dingtalk-hook`
- 配置与模板安装到 `/etc/promethues-DingTalk-Hook/`
- 注册并启动 systemd 服务 `prometheus-dingtalk-hook.service`

```bash
curl -fsSL https://raw.githubusercontent.com/NicoOrz/promethues-DingTalk-Hook/unstable/install.sh | sh
```

安装完成后会提示你：
- 配置文件路径（默认：`/etc/promethues-DingTalk-Hook/config.yml`）
- 服务名（默认：`prometheus-dingtalk-hook.service`）
- 常用命令（重启/日志）

卸载（默认保留 `/etc/promethues-DingTalk-Hook/` 配置目录）：

```bash
curl -fsSL https://raw.githubusercontent.com/NicoOrz/promethues-DingTalk-Hook/unstable/install.sh | sh -s uninstall
```

彻底卸载（包含配置目录）：

```bash
curl -fsSL https://raw.githubusercontent.com/NicoOrz/promethues-DingTalk-Hook/unstable/install.sh | PURGE=1 sh -s uninstall
```

## 管理 UI（可选）

启用示例：

```yaml
admin:
  enabled: true
  path_prefix: "/admin"
  basic_auth:
    username: "admin"
    password: "change-me"
```

## 安全提示

- 不要在公开仓库中泄露 `access_token`、`secret`、`auth.token`
- 建议置于内网或启用 token 鉴权

# Prometheus DingTalk Hook

一个轻量的 Go Webhook：接收 Prometheus Alertmanager Webhook（JSON），按模板渲染后转发到钉钉群机器人（支持加签、多机器人、按 receiver 路由，可选 token 鉴权）。

## 功能
- 接收 Alertmanager Webhook（/alert）
- 多钉钉机器人配置
- 路由：支持 legacy `receiver` 路由，也支持 `channels + routes`（按 receiver/labels 匹配）
- 支持钉钉 `@全体/@手机号`（按渠道默认规则 + 规则匹配）
- 支持钉钉“加签”安全设置（secret）
- 支持 Go `text/template` 自定义消息模板（单文件/模板目录多模板）
- 健康检查：`/healthz`、`/readyz`
- 可选 token 鉴权（Bearer / X-Token）
- 安全热加载：轮询检测变更 + 手动 `POST /-/reload`（失败回滚）
- 管理 UI：`/admin/`（Basic Auth），支持模板管理/预览、测试发送、配置导入导出

## 快速开始

### 1) 配置
复制示例配置：
```bash
cp configs/config.example.yaml config.yaml
```

编辑 `config.yaml`，至少配置：
- `dingtalk.robots[0].webhook`
- （二选一）
  - legacy：`dingtalk.receivers.default`
  - 推荐：`dingtalk.channels` 中包含 `name: "default"` 且绑定至少一个机器人

### 2) 运行（二进制）
```bash
go build -o prometheus-dingtalk-hook ./cmd/prometheus-dingtalk-hook
./prometheus-dingtalk-hook -config ./config.yaml
```

### 3) 运行（Docker Compose）
在项目根目录（已包含 `docker-compose.yml`，并按上文创建好 `config.yaml`）中执行：
```bash
docker compose up -d
```

### 4) 构建镜像（可选）
Dockerfile 仅用于构建镜像（GoReleaser/本地构建）；运行推荐使用上面的 `docker compose`。
```bash
go build -o prometheus-dingtalk-hook ./cmd/prometheus-dingtalk-hook
docker build -t prometheus-dingtalk-hook:local .
docker run --rm -p 8080:8080 -v "$PWD/config.yaml:/app/config.yaml:ro" -v "$PWD/templates:/app/templates:ro" prometheus-dingtalk-hook:local
```

## Alertmanager 配置示例
在 Alertmanager 中将 webhook 指向本服务：

```yaml
receivers:
  - name: ops-team
    webhook_configs:
      - url: "http://prometheus-dingtalk-hook:8080/alert"
        send_resolved: true
```

如果启用 token 鉴权，建议使用 `Authorization: Bearer`（Alertmanager 支持方式以实际版本为准）；也可以使用 `X-Token: <token>`。

## 模板
默认模板内置在二进制中。如需自定义：

1) 单文件模板（legacy）：准备模板文件（可基于 `templates/default.tmpl` 修改），配置：
```yaml
template:
  file: "templates/custom.tmpl"
```

2) 模板目录（推荐，支持多模板/管理 UI）：
```yaml
template:
  dir: "templates"
  default: "default"
```

3) 变更模板后可通过 `POST /-/reload` 或 `/admin/api/v1/reload` 生效（如启用轮询热加载则会自动生效）。

## 路由（推荐：channels + routes）

当 `dingtalk.channels` 非空时，服务优先使用 `channels + routes` 做路由；否则使用 legacy `dingtalk.receivers`（兼容旧配置，运行时会转换为等价的 channels/routes）。

### 1) 最小可用示例

```yaml
template:
  dir: "templates"
  default: "default"

dingtalk:
  robots:
    - name: "robot-ops"
      webhook: "https://oapi.dingtalk.com/robot/send?access_token=YOUR_ACCESS_TOKEN"
      secret: ""
      msg_type: "markdown"
      title: "Alertmanager"

  channels:
    - name: "default"      # 必须存在
      robots: ["robot-ops"]
      template: "default"  # 对应 templates/default.tmpl（也可使用内置 default）

    - name: "ops"
      robots: ["robot-ops"]
      template: "ops"      # 对应 templates/ops.tmpl
      mention_rules:
        - name: "critical->@all"
          when:
            labels:
              severity: ["critical"]
          mention:
            at_all: true

  routes:
    - name: "by-receiver"
      when:
        receiver: ["ops-team"]
      channels: ["ops"]
    - name: "by-severity"
      when:
        labels:
          severity: ["critical"]
      channels: ["ops"]
```

说明：
- `routes` 按顺序匹配，命中后发送到对应 `channels`
- 未命中任何 `routes` 时回落到 `default` channel
- `when` 支持 `receiver/status/labels`（AND 语义），`labels` 的 value 列表为“命中任一即命中”
- `channel.template` 取模板名（不含 `.tmpl`），来自 `template.dir` 目录下的 `*.tmpl`（同时内置 `default` 永远可用）

### 2) mentions（@全体/@手机号/@用户ID）

```yaml
dingtalk:
  channels:
    - name: "ops"
      robots: ["robot-ops"]
      template: "ops"
      mention:
        at_all: false
        at_mobiles: ["13800138000"]
        at_user_ids: ["user123"]
      mention_rules:
        - name: "critical->@all"
          when:
            labels:
              severity: ["critical"]
          mention:
            at_all: true
```

说明：钉钉自定义机器人 `atMobiles/atUserIds` 需要配合消息内容里的 `@xxx` 才能生效，本项目会在消息末尾自动追加对应的 `@` 文本。

### 3) 哪些改动支持热加载
- 支持热加载：`auth.token`、`dingtalk.*`、`template.*`、`admin.enabled/basic_auth`、mentions/routes/channels（校验通过后原子切换，失败回滚）
- 需要重启才能生效：`server.listen`、`server.path`、`admin.path_prefix`

## 管理 UI（可选）

启用 Basic Auth 后访问：`/admin/`。

```yaml
admin:
  enabled: true
  path_prefix: "/admin"
  basic_auth:
    username: "admin"
    password: "change-me"
```

## 安全提示
- 不要在仓库或日志中泄露 `access_token`、`secret`、`auth.token`
- 建议将配置文件权限设为仅运行用户可读

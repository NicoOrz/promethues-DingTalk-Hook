# Prometheus DingTalk Hook

一个轻量的 Go Webhook 服务：

- 接收 Prometheus Alertmanager Webhook（JSON）
- 使用 Go `text/template` 渲染消息
- 转发到钉钉群机器人

## 功能

- 多钉钉机器人配置
- 路由：按 receiver/status/labels 匹配告警发送规则
- @：`@all` / `@手机号` / `@userId`
- 可选 token 鉴权
- 可视化配置 UI

## QuickStart
### 一键安装

脚本将：
- 自动下载最新 Release
- 二进制安装到 `/usr/local/bin/prometheus-dingtalk-hook`
- 配置与模板安装到 `/etc/prometheus-DingTalk-Hook/`
- 注册并启动 systemd 服务 `prometheus-dingtalk-hook.service`

```bash
curl -fsSL https://raw.githubusercontent.com/NicoOrz/prometheus-DingTalk-Hook/main/install.sh | sh
```

安装指定版本（例如 `v1.2.3`）：

```bash
curl -fsSL https://raw.githubusercontent.com/NicoOrz/prometheus-DingTalk-Hook/main/install.sh | VERSION=v1.2.3 sh
```
### Docker 运行


1) 准备配置文件：

```bash
cp config.example.yml config.yml
```

配置文件需满足：
- `server.listen` 为 `"0.0.0.0:9098"`
- `dingtalk.robots[0].webhook` 已正确配置
- `dingtalk.channels` 中包含 `name: "default"` 且绑定至少一个机器人

2) 启动：

```bash
docker compose up -d
```




### 二进制安装


1) 克隆仓库&创建配置：

```bash
git clone https://github.com/NicoOrz/prometheus-DingTalk-Hook.git
cp config.example.yml config.yml
```

2) 编辑 `config.yml`：

- 设置 `dingtalk.robots[0].webhook`
- 确保 `dingtalk.channels` 中包含 `name: "default"` 且绑定至少一个机器人

3) 运行：

```bash
go run ./cmd/prometheus-dingtalk-hook --config.file config.yml
```


## 管理 UI

启用示例：

```yaml
admin:
  enabled: true
  path_prefix: "/admin"
  basic_auth:
    username: "admin"
    password: "change-me"
```

## 模板

二进制内置 `default` 模板。

自定义模板支持配置模板文件夹 `template.dir`：加载目录下的 `*.tmpl`.

```yaml
template:
  dir: "templates"
```

模板解析逻辑：

- `template.dir` 为空：使用内置 `default` 模板
- `template.dir` 指向的目录不存在：回退使用内置 `default` 模板
- `channels[].template` 填写模板名，`default` 对应 `default.tmpl`
## Alertmanager 配置示例

```yaml
receivers:
  - name: ops-team
    webhook_configs:
      - url: "http://127.0.0.1:9098/alert"
        send_resolved: true

```
## 钉钉消息标题

当机器人 `msg_type: "markdown"` 时，`dingtalk.robots[].title` 对应钉钉 `markdown.title`。

`title` 为空时，`markdown.title` 取值顺序如下：

- `commonAnnotations.summary`
- `alerts[0].annotations.summary`
- `commonLabels.alertname`
- `alerts[0].labels.alertname`
- `"Alertmanager"`





## 卸载
卸载，保留 `/etc/prometheus-DingTalk-Hook/`配置：

```bash
curl -fsSL https://raw.githubusercontent.com/NicoOrz/prometheus-DingTalk-Hook/main/install.sh | sh -s uninstall
```

彻底卸载，删除 `/etc/prometheus-DingTalk-Hook/` 配置：

```bash
curl -fsSL https://raw.githubusercontent.com/NicoOrz/prometheus-DingTalk-Hook/main/install.sh | PURGE=1 sh -s uninstall
```

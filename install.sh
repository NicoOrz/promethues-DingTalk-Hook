#!/bin/sh
set -eu

OWNER="NicoOrz"
REPO="promethues-DingTalk-Hook"
PROJECT="promethues-DingTalk-Hook"
BIN="prometheus-dingtalk-hook"
SERVICE="prometheus-dingtalk-hook"

ETC_DIR="/etc/promethues-DingTalk-Hook"
CFG_DST="${ETC_DIR}/config.yml"
TPL_DST_DIR="${ETC_DIR}/templates"
BIN_DST="/usr/share/bin/${BIN}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "错误: 缺少命令 $1" >&2; exit 1; }; }
as_root() { [ "$(id -u)" -eq 0 ] && "$@" || sudo "$@"; }

need curl
need tar
need uname
need mktemp
need grep
need awk

if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
  echo "错误: 当前系统不是 systemd 驱动，无法安装为 systemd 服务。" >&2
  exit 1
fi

if [ "$(id -u)" -ne 0 ] && ! command -v sudo >/dev/null 2>&1; then
  echo "错误: 需要 root 权限（请用 root 运行或安装 sudo）。" >&2
  exit 1
fi

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch="x86_64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "错误: 不支持的架构: $(uname -m)" >&2; exit 1 ;;
esac

tag="$(curl -fsSLI "https://github.com/${OWNER}/${REPO}/releases/latest" | awk 'tolower($1)=="location:"{print $2}' | tail -n 1 | tr -d '\r')"
tag="${tag##*/tag/}"
tag="${tag##*/}"
[ -n "$tag" ] || { echo "错误: 无法获取最新 release 版本号" >&2; exit 1; }

version="${tag#v}"
archive="${PROJECT}_${version}_Linux_${arch}.tar.gz"
base="https://github.com/${OWNER}/${REPO}/releases/download/${tag}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "下载 release: ${tag}"
curl -fsSL "${base}/${archive}" -o "${tmp}/${archive}"
tar -xzf "${tmp}/${archive}" -C "$tmp"

[ -f "${tmp}/${BIN}" ] || { echo "错误: 归档中未找到二进制 ${BIN}" >&2; exit 1; }
[ -f "${tmp}/config.example.yml" ] || { echo "错误: 归档中未找到 config.example.yml" >&2; exit 1; }
[ -d "${tmp}/templates" ] || { echo "错误: 归档中未找到 templates/ 目录" >&2; exit 1; }

as_root mkdir -p "$ETC_DIR" "$TPL_DST_DIR" "$(dirname "$BIN_DST")"
as_root install -m 0755 "${tmp}/${BIN}" "$BIN_DST"

if [ ! -f "$CFG_DST" ]; then
  as_root cp "${tmp}/config.example.yml" "$CFG_DST"
  echo "已生成配置: ${CFG_DST}"
else
  echo "已存在配置: ${CFG_DST}（跳过覆盖）"
fi

as_root cp -n "${tmp}/templates/"*.tmpl "$TPL_DST_DIR/" 2>/dev/null || true

UNIT="/etc/systemd/system/${SERVICE}.service"
as_root sh -c "cat >\"$UNIT\" <<EOF
[Unit]
Description=Prometheus DingTalk Hook
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN_DST} -config ${CFG_DST}
Restart=on-failure
RestartSec=2s

[Install]
WantedBy=multi-user.target
EOF"

as_root systemctl daemon-reload
as_root systemctl enable --now "${SERVICE}.service"

echo ""
echo "✅ 安装完成"
echo ""
echo "配置指引："
echo "1) 编辑配置：${CFG_DST}"
echo "2) 重启服务：systemctl restart ${SERVICE}"
echo "3) 查看日志：journalctl -u ${SERVICE} -f"
echo ""
echo "模板目录：${TPL_DST_DIR}（可放置 *.tmpl，config 中 template.dir 可按需填写）"


#!/bin/sh
set -eu

OWNER="NicoOrz"
REPO="prometheus-DingTalk-Hook"
PROJECT="prometheus-DingTalk-Hook"
BIN="prometheus-dingtalk-hook"
SERVICE="prometheus-dingtalk-hook"

ETC_DIR="/etc/prometheus-DingTalk-Hook"
CFG_DST="${ETC_DIR}/config.yml"
TPL_DST_DIR="${ETC_DIR}/templates"
BIN_DST="${BIN_DST:-/usr/local/bin/${BIN}}"
VERSION="${VERSION:-latest}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "错误: 缺少命令 $1" >&2; exit 1; }; }
as_root() { [ "$(id -u)" -eq 0 ] && "$@" || sudo "$@"; }

uninstall() {
  need grep
  need awk

  if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
    echo "错误: 当前系统不是 systemd 驱动，无法卸载 systemd 服务。" >&2
    exit 1
  fi

  if [ "$(id -u)" -ne 0 ] && ! command -v sudo >/dev/null 2>&1; then
    echo "错误: 需要 root 权限（请用 root 运行或安装 sudo）。" >&2
    exit 1
  fi

  UNIT="/etc/systemd/system/${SERVICE}.service"

  if systemctl list-unit-files | awk '{print $1}' | grep -qx "${SERVICE}.service"; then
    as_root systemctl disable --now "${SERVICE}.service" >/dev/null 2>&1 || true
  else
    as_root systemctl stop "${SERVICE}.service" >/dev/null 2>&1 || true
  fi

  as_root rm -f "$UNIT"
  as_root systemctl daemon-reload

  as_root rm -f "$BIN_DST"

  if [ "${PURGE:-0}" = "1" ]; then
    as_root rm -rf "$ETC_DIR"
    echo "已清理配置目录: ${ETC_DIR}"
  else
    echo "已保留配置目录: ${ETC_DIR}（如需删除请添加 PURGE=1）"
  fi

  echo "✅ 卸载完成"
  exit 0
}

cmd="${1:-install}"
case "$cmd" in
  install) ;;
  uninstall) uninstall ;;
  *) echo "用法: $0 [install|uninstall]" >&2; exit 2 ;;
esac

need uname
need mktemp

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

	need curl
	need tar

	if [ "$VERSION" = "latest" ]; then
	  need awk

	  tag="$(curl -fsSL "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" 2>/dev/null | awk -F'"' '/"tag_name":/ {print $4; exit}')"
	  if [ -z "$tag" ]; then
	    tag="$(curl -fsSLI "https://github.com/${OWNER}/${REPO}/releases/latest" | awk 'tolower($1)=="location:"{print $2}' | tail -n 1 | tr -d '\r')"
	    tag="${tag##*/tag/}"
	    tag="${tag##*/}"
	  fi
	  [ -n "$tag" ] || { echo "错误: 无法获取最新 release 版本号（可通过 VERSION=v1.2.3 指定版本）" >&2; exit 1; }
	else
	  tag="$VERSION"
	fi

	case "$tag" in v*) : ;; *) tag="v$tag" ;; esac

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
	  echo "已存在配置: ${CFG_DST}"
	fi
	
	copied_any=0
	for tmpl in "${tmp}/templates/"*.tmpl; do
	  [ -f "$tmpl" ] || continue
	  copied_any=1
	  as_root cp -n "$tmpl" "$TPL_DST_DIR/" || echo "警告: 复制模板失败: $tmpl" >&2
	done
	[ "$copied_any" -eq 1 ] || echo "警告: 未找到任何 *.tmpl 模板文件" >&2
	
	UNIT="/etc/systemd/system/${SERVICE}.service"
	as_root sh -c "cat >\"$UNIT\" <<EOF
[Unit]
Description=Prometheus DingTalk Hook
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${BIN_DST} --config.file ${CFG_DST}
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
echo "开始使用："
echo "1) 编辑配置：${CFG_DST}"
echo "2) 重启服务：systemctl restart ${SERVICE}"
echo "模板目录：${TPL_DST_DIR}（可放置 *.tmpl；默认配置已指向该目录）"
echo "3) 查看日志：journalctl -u ${SERVICE} -f"

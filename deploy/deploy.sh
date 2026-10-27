#!/usr/bin/env bash
# 统一授权平台 · CentOS 9 一键部署脚本
# 用法：sudo ./deploy.sh
# 首次运行会生成 .env（含主密钥与初始管理员），并完成构建、启动与健康检查。
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

ENV_FILE="$SCRIPT_DIR/.env"
DATA_DIR="$SCRIPT_DIR/data"
SERVICE="ulp"
PORT="7630"

log()  { printf '\033[32m[deploy]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[deploy]\033[0m %s\n' "$*"; }
die()  { printf '\033[31m[deploy] 错误：\033[0m%s\n' "$*" >&2; exit 1; }

rand_hex() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$1"
  else
    head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

# ---------- 1. 环境检查 ----------
log "检查运行环境…"
command -v docker >/dev/null 2>&1 || die "未安装 Docker Engine，请先安装 Docker 24+（CentOS 9 参见 docs/部署手册-CentOS9.md）"
docker info >/dev/null 2>&1 || die "无法连接 Docker 守护进程，请确认已启动：systemctl enable --now docker"
docker compose version >/dev/null 2>&1 || die "缺少 docker compose v2 插件，请安装 docker-compose-plugin"
command -v curl >/dev/null 2>&1 || warn "未找到 curl，健康检查将改用容器状态判断（不影响部署）"

# ---------- 2. 生成配置 ----------
if [[ -f "$ENV_FILE" ]]; then
  log "检测到已有 .env，沿用现有主密钥与管理员配置"
  # shellcheck disable=SC1090
  set -a; . "$ENV_FILE"; set +a
else
  ADMIN_USER="${ULP_ADMIN_USER:-admin}"
  if [[ -z "${ULP_ADMIN_PASSWORD:-}" ]]; then
    # 字母（Ulp 前缀）+ 数字（固定 9）均满足强度要求。
    ADMIN_PASSWORD="Ulp$(rand_hex 9)9"
  else
    ADMIN_PASSWORD="$ULP_ADMIN_PASSWORD"
  fi
  MASTER_KEY="$(rand_hex 32)"
  JWT_SECRET="$(rand_hex 32)"

  umask 077
  cat > "$ENV_FILE" <<EOF
# 统一授权平台运行配置（由 deploy.sh 生成，请妥善保管并做好备份）
ULP_MASTER_KEY=$MASTER_KEY
ULP_JWT_SECRET=$JWT_SECRET
ULP_ADMIN_USER=$ADMIN_USER
ULP_ADMIN_PASSWORD=$ADMIN_PASSWORD
ULP_TOKEN_TTL=12h
EOF
  chmod 600 "$ENV_FILE"
  log "已生成 $ENV_FILE（权限 600）"
fi

mkdir -p "$DATA_DIR/licenses"
# 容器内以非 root 用户 ulp(10001) 运行，数据卷需匹配属主。
chown -R 10001:10001 "$DATA_DIR"
chmod 700 "$DATA_DIR" "$DATA_DIR/licenses"

# ---------- 3. 构建并启动 ----------
log "构建镜像（Go 静态编译，CGO_ENABLED=0）…"
docker compose build

log "启动服务…"
docker compose up -d

# ---------- 4. 等待健康检查 ----------
log "等待服务就绪…"
READY=0
for _ in $(seq 1 60); do
  STATUS="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$SERVICE" 2>/dev/null || echo unknown)"
  if [[ "$STATUS" == "healthy" ]]; then READY=1; break; fi
  if [[ "$STATUS" == "unhealthy" ]]; then
    docker compose logs --tail=60 "$SERVICE" || true
    die "容器健康检查失败，请查看上方日志"
  fi
  sleep 2
done
if [[ "$READY" -ne 1 ]]; then
  docker compose logs --tail=60 "$SERVICE" || true
  die "服务在 120 秒内未就绪"
fi

if command -v curl >/dev/null 2>&1; then
  curl -fsS "http://127.0.0.1:$PORT/api/health" >/dev/null || die "健康接口不可访问"
fi

# ---------- 5. 输出访问信息 ----------
HOST_IP="$(hostname -I 2>/dev/null | awk '{print $1}')"
[[ -n "$HOST_IP" ]] || HOST_IP="<服务器IP>"

SHOW_USER="${ULP_ADMIN_USER:-admin}"
SHOW_PASSWORD="${ULP_ADMIN_PASSWORD:-${ADMIN_PASSWORD:-}}"
[[ -n "$SHOW_PASSWORD" ]] || SHOW_PASSWORD="（请查看 $ENV_FILE）"

cat <<EOF

============================================================
 统一授权平台已启动
   访问地址：http://$HOST_IP:$PORT
   本机地址：http://127.0.0.1:$PORT
   初始账号：$SHOW_USER
   初始密码：$SHOW_PASSWORD
   数据目录：$DATA_DIR（SQLite + 授权文件 + 回拨检查点）
============================================================
 首次登录后必须修改密码。
 如外网无法访问，请放行防火墙：
   firewall-cmd --permanent --add-port=$PORT/tcp && firewall-cmd --reload
 查看日志：cd $SCRIPT_DIR && docker compose logs -f
 停止服务：cd $SCRIPT_DIR && docker compose down
============================================================

EOF

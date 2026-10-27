# 统一授权平台 · 部署说明（CentOS 9）

## 1. 前置要求

| 组件 | 版本要求 | 说明 |
|---|---|---|
| Docker Engine | 24+（推荐 26+） | 提供容器运行时 |
| docker compose | v2 插件（`docker compose`，非 `docker-compose`） | 脚本依赖 v2 语法 |
| 操作系统 | CentOS Stream 9 / RHEL 9 x86_64 | 已在 CentOS 9 验证 |
| 端口 | 7630/TCP | 平台访问端口 |
| 磁盘 | ≥ 1 GB | 数据库与授权文件均很小 |

安装 Docker（官方仓库）：

```bash
sudo dnf -y install dnf-plugins-core
sudo dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
sudo dnf -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
```

## 2. 一键部署

```bash
cd unified-license-platform/deploy
sudo ./deploy.sh
```

脚本会依次完成：

1. 检查 Docker、compose v2；
2. 首次运行时生成 `.env`：`ULP_MASTER_KEY`（64 位随机十六进制）、`ULP_JWT_SECRET`、初始管理员账号密码；
3. 创建 `data/licenses`，并把数据目录属主调整为容器用户 `10001`；
4. `docker compose build` → `docker compose up -d`；
5. 轮询容器健康状态，最长等待 120 秒；
6. 输出访问地址与初始凭据。

首次登录会强制修改密码，修改成功后进入概览看板。

> 自定义初始管理员：部署前导出 `ULP_ADMIN_USER` / `ULP_ADMIN_PASSWORD` 即可，密码需为 10~128 位且同时包含字母与数字。

## 3. 防火墙（firewalld）

CentOS 9 默认启用 firewalld，需放行 7630：

```bash
sudo firewall-cmd --permanent --add-port=7630/tcp
sudo firewall-cmd --reload
sudo firewall-cmd --list-ports
```

## 4. SELinux

`docker-compose.yml` 已为数据卷声明 `:z` 标签，Docker 会自动为宿主机目录重新打上 `container_file_t`：

```yaml
volumes:
  - ./data:/data:z
```

若仍出现 `Permission denied`：

```bash
sudo semanage fcontext -a -t container_file_t "/opt/四个项目/unified-license-platform/deploy/data(/.*)?"
sudo restorecon -Rv /opt/四个项目/unified-license-platform/deploy/data
```

不建议关闭 SELinux（`setenforce 0`），如需排查可临时执行并在完成后恢复。

## 5. 数据持久化与备份

所有持久化数据位于 `deploy/data/`：

| 文件 | 内容 |
|---|---|
| `ulpd.db` | SQLite：项目、密钥密文、授权记录、管理员、审计日志 |
| `licenses/*.lic` | 已签发的授权文件 |
| `clock.dat` | 时间回拨检查点（平台自身使用） |

备份：停止服务后打包即可，注意 `.env` 中的主密钥必须一起备份（丢失将无法解密私钥）。

```bash
docker compose down
tar czf ulp-backup-$(date +%F).tar.gz .env data
docker compose up -d
```

`.env` 权限必须保持 `600`，且**不得**提交到版本库。

## 6. 常用运维命令

```bash
docker compose ps                 # 状态与健康检查
docker compose logs -f --tail=100 # 日志
docker compose restart            # 重启
docker compose down               # 停止并移除容器（数据保留）
docker compose build --no-cache   # 完全重建镜像
```

升级：替换源码后执行 `docker compose build && docker compose up -d`，数据库迁移在启动时自动完成。

## 7. 离线验签 CLI

镜像内附带 `ulp-verify`，可在无网络环境验证授权文件：

```bash
docker run --rm -v /path/to/license:/lic:ro unified-license-platform:1.0.0 \
  ulp-verify -lic /lic/xxx.lic -pub /lic/project-public.pem -product agent-system
```

有效时退出码为 `0`，无效为 `2`，并输出 JSON 格式的 `result` 与 `license`。

## 8. 故障排查

| 现象 | 排查方向 |
|---|---|
| 容器反复重启，日志提示缺少 `ULP_MASTER_KEY` | `.env` 缺失或被覆盖，执行 `docker compose config` 检查 |
| 首次启动报“必须注入初始管理员” | 数据库已有管理员但 `.env` 无对应账号时不会重建；如需重置请备份后删除 `data/ulpd.db` |
| 页面 404 / 空白 | 确认镜像包含内嵌前端：`docker run --rm <image> ls /app` 非必要；直接访问 `http://IP:7630/` |
| 外网无法访问 | 检查 `firewall-cmd --list-ports`、云厂商安全组、SELinux 标签 |
| 健康检查失败 | `docker compose logs`，确认 7630 未被占用（`ss -lntp \| grep 7630`） |

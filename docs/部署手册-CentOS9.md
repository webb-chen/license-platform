# 统一授权平台 · 部署手册（CentOS 9）

本文档描述在 **CentOS Stream 9 / RHEL 9 x86_64** 上使用 Docker 部署统一授权平台（ULP）的完整步骤，包括环境准备、一键部署、防火墙与 SELinux、数据备份恢复、升级和故障排查。

## 1. 部署架构

```text
浏览器 ──HTTP/HTTPS──> 7630/TCP
                        │
                        ▼
              ┌───────────────────┐
              │  容器 ulp (ulpd)  │  非 root 用户 10001
              │  Go 静态二进制    │  内嵌中文管理界面
              └─────────┬─────────┘
                        │ 绑定挂载 ./data:/data:z
                        ▼
        ┌──────────────────────────────────┐
        │ ulpd.db（SQLite）                │
        │ licenses/*.lic（已签发授权）     │
        │ clock.dat（时间回拨检查点）      │
        └──────────────────────────────────┘
```

- 单个容器、单个二进制，无外部数据库；
- 前端资源通过 `go:embed` 内嵌，无需额外 Web 服务器；
- 数据卷与 `deploy/.env` 必须一起备份。

## 2. 环境要求

| 项目 | 要求 |
| --- | --- |
| 操作系统 | CentOS Stream 9 / RHEL 9 x86_64 |
| CPU / 内存 | 1 核 / 1 GB 起 |
| 磁盘 | ≥ 1 GB（数据量很小） |
| Docker Engine | 24+（推荐 26+） |
| Docker Compose | v2 插件（`docker compose`） |
| 端口 | 7630/TCP |
| 网络 | 首次构建需访问镜像仓库与 Go 模块代理 |

## 3. 安装 Docker 与 Compose

```bash
sudo dnf -y install dnf-plugins-core
sudo dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
sudo dnf -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
sudo systemctl enable --now docker
docker --version
docker compose version
```

若服务器无法直连 Docker Hub，请先配置镜像加速器（`/etc/docker/daemon.json`）：

```json
{
  "registry-mirrors": ["https://<你的镜像加速地址>"]
}
```

```bash
sudo systemctl daemon-reload && sudo systemctl restart docker
```

## 4. 上传代码并一键部署

将项目目录上传至服务器，例如 `/opt/unified-license-platform`：

```bash
cd /opt/unified-license-platform/deploy
sudo ./deploy.sh
```

`deploy.sh` 的执行内容：

1. 检查 Docker 守护进程与 Compose v2；
2. 首次运行生成 `.env`（权限 600）：
   - `ULP_MASTER_KEY`：64 位随机十六进制，用于 AES-256-GCM 加密私钥；
   - `ULP_JWT_SECRET`：64 位随机十六进制，用于登录令牌签名；
   - `ULP_ADMIN_USER`：默认 `admin`；
   - `ULP_ADMIN_PASSWORD`：随机初始密码；
3. 创建 `data/licenses`，并将 `data/` 属主调整为容器用户 `10001`；
4. `docker compose build`（多阶段构建，`CGO_ENABLED=0` 静态编译）；
5. `docker compose up -d`；
6. 轮询健康检查（最长 120 秒），输出访问地址与初始凭据。

自定义初始账号（密码需 10~128 位且同时包含字母和数字）：

```bash
sudo ULP_ADMIN_USER=admin ULP_ADMIN_PASSWORD='YourPass12345' ./deploy.sh
```

> 部署完成后请立即登录并修改初始密码；平台会强制首次改密。

## 5. 防火墙（firewalld）

```bash
sudo firewall-cmd --permanent --add-port=7630/tcp
sudo firewall-cmd --reload
sudo firewall-cmd --list-ports
```

云服务器还需在安全组中放行 7630/TCP。生产环境建议只允许办公网段访问：

```bash
sudo firewall-cmd --permanent --add-rich-rule='rule family="ipv4" source address="10.0.0.0/8" port port="7630" protocol="tcp" accept'
sudo firewall-cmd --reload
```

## 6. SELinux

`docker-compose.yml` 已为数据卷声明 `:z`，Docker 会自动打上 `container_file_t` 标签：

```yaml
volumes:
  - ./data:/data:z
```

若日志出现 `permission denied`，执行：

```bash
sudo semanage fcontext -a -t container_file_t "/opt/unified-license-platform/deploy/data(/.*)?"
sudo restorecon -Rv /opt/unified-license-platform/deploy/data
```

不要长期关闭 SELinux。临时排查可用 `sudo setenforce 0`，完成后执行 `sudo setenforce 1`。

## 7. 部署验证

```bash
# 容器状态应为 healthy
docker compose ps

# 健康接口应返回 status=ok
curl -fsS http://127.0.0.1:7630/api/health

# 管理界面
curl -fsS http://127.0.0.1:7630/ | head -n 5
```

浏览器访问 `http://<服务器IP>:7630`，完成以下验收：

1. 使用初始账号登录，确认跳转强制改密页；
2. 修改密码后进入概览看板；
3. 新建项目，确认生成公钥 PEM 与三语言接入片段；
4. 分别签发“指定日期 / 自定义天数 / 永久”三种授权；
5. 下载 `.lic`，确认文件名为 `<项目ID>-<客户>-<licenseID>.lic`；
6. 使用项目公钥离线验签，确认 `ok`；
7. 吊销授权，确认下载被拒绝（HTTP 409）；
8. 续期授权，确认生成新记录且原记录为 `superseded`；
9. 查看审计日志，确认上述操作均有记录。

## 8. 数据备份与恢复

所有持久化数据位于 `deploy/data/`：

| 文件 | 内容 |
| --- | --- |
| `ulpd.db` | 项目、密钥密文、授权记录、管理员、审计日志 |
| `licenses/*.lic` | 已签发的授权文件 |
| `clock.dat` | 时间回拨检查点 |

**备份（建议每日定时）：**

```bash
cd /opt/unified-license-platform/deploy
docker compose down
sudo tar czf /var/backups/ulp-$(date +%F).tar.gz .env data
docker compose up -d
```

**恢复：**

```bash
cd /opt/unified-license-platform/deploy
docker compose down
sudo tar xzf /var/backups/ulp-YYYY-MM-DD.tar.gz
sudo chown -R 10001:10001 data
docker compose up -d
```

> `ULP_MASTER_KEY` 丢失后，数据库中已加密的私钥将无法解密，表现为无法继续签发授权；因此 `.env` 必须与数据库同等保护并一起备份。`.env` 权限保持 `600`，禁止提交到版本库。

## 9. 日常运维

```bash
cd /opt/unified-license-platform/deploy
docker compose ps                    # 状态与健康
docker compose logs -f --tail=200    # 实时日志
docker compose restart               # 重启
docker compose down                  # 停止（数据保留）
docker compose up -d                 # 启动
docker compose build --no-cache      # 完全重建镜像
```

**升级：**

```bash
cd /opt/unified-license-platform
sudo tar czf /var/backups/ulp-before-upgrade.tar.gz deploy/.env deploy/data
cd deploy
docker compose build && docker compose up -d
```

数据库迁移在服务启动时自动执行（`CREATE TABLE IF NOT EXISTS` 与索引创建）。

## 10. 离线验签

镜像内附带 `ulp-verify`，可在无网络环境验证授权：

```bash
docker run --rm \
  -v /path/to/files:/lic:ro \
  unified-license-platform:1.0.0 \
  ulp-verify -lic /lic/xxx.lic -pub /lic/project-public.pem -product agent-system
```

- 有效（`ok` / `license_grace`）退出码为 `0`；
- 无效退出码为 `2`，输出包含统一状态码的 JSON。

常用参数：

| 参数 | 说明 |
| --- | --- |
| `-lic` | 授权文件路径 |
| `-pub` | 项目公钥 PEM 路径 |
| `-product` | 期望的 product 标识 |
| `-machine` | 指定机器码（默认自动计算） |
| `-clock` | 时间回拨检查点文件（启用回拨检测） |
| `-print-machine` | 仅打印本机机器码 |

## 11. 反向代理与 HTTPS（推荐）

生产环境建议在 ULP 前放置 Nginx 并启用 HTTPS：

```nginx
server {
    listen 443 ssl;
    server_name license.example.com;

    ssl_certificate     /etc/nginx/ssl/license.crt;
    ssl_certificate_key /etc/nginx/ssl/license.key;

    client_max_body_size 10m;

    location / {
        proxy_pass http://127.0.0.1:7630;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

启用 HTTPS 后可将 7630 端口限制为仅本机访问。

## 12. 故障排查

| 现象 | 排查方向 |
| --- | --- |
| 容器反复重启，日志提示 `缺少环境变量 ULP_MASTER_KEY` | `deploy/.env` 缺失或被覆盖；执行 `docker compose config` 检查 |
| 日志提示 `mkdir /data/licenses: permission denied` | 宿主机 `data/` 属主不正确；执行 `sudo chown -R 10001:10001 deploy/data` |
| 日志提示 `首次启动必须通过 ULP_ADMIN_USER / ULP_ADMIN_PASSWORD 注入初始管理员` | 数据库为空且未提供初始账号；在 `.env` 中补充后重启（仅首次生效） |
| 构建时 `proxy.golang.org i/o timeout` | Dockerfile 默认使用 `GOPROXY=https://goproxy.cn,direct`；如仍失败，配置网络或 `--build-arg GOPROXY=<可用代理>` |
| 页面 404 / 空白 | 访问 `http://IP:7630/`；确认镜像构建成功并包含内嵌前端 |
| 外网无法访问 | 检查 `firewall-cmd --list-ports`、云安全组、SELinux 标签 |
| 健康检查失败 | `docker compose logs`；确认 7630 未被占用：`ss -lntp \| grep 7630` |
| 修改密码后仍提示改密 | 清理浏览器本地令牌后重新登录 |

## 13. 卸载

```bash
cd /opt/unified-license-platform/deploy
docker compose down
docker image rm unified-license-platform:1.0.0
# 确认已备份后再删除数据
# sudo rm -rf data .env
```

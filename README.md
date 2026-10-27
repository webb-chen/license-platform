# 统一授权平台（Unified License Platform）

统一授权平台为多个业务项目提供**离线授权签发、分发、吊销与验签**能力。平台以单个 Go 二进制运行，内置中文管理界面，数据（SQLite、`.lic` 文件、时间回拨检查点）统一落在同一个数据卷，适合在内网或隔离环境部署。

- 默认端口：**7630**
- 授权格式：`ULP License Format v1`（外层信封 `{data, sign}`，原始字节 RSA-2048 / SHA-256 验签）
- 运行时依赖：无（`CGO_ENABLED=0` 静态编译，SQLite 使用纯 Go 实现）
- 数据存储：`<数据目录>/ulpd.db` + `<数据目录>/licenses/*.lic` + `<数据目录>/clock.dat`

## 功能

| 能力 | 说明 |
| --- | --- |
| 概览看板 | 授权总量、生效、宽限、过期、吊销、替代、项目数、近期到期与最近操作 |
| 项目管理 | 动态增删改查；每个项目自动生成独立 RSA-2048 密钥对 |
| 密钥管理 | 公钥 PEM / 指纹 / Go、Python、Node 接入片段；支持轮换（旧公钥停用） |
| 授权签发 | 指定日期时间（精确到分钟）、自定义天数、永久三种到期模式；可配置客户、联系人、机器绑定、宽限期、功能备注 |
| 授权记录 | 状态筛选、分页、详情预览、`.lic` 下载、吊销、续期、删除 |
| 审计日志 | 登录、改密、项目、签发、下载、吊销、续期、密钥轮换等操作留痕，支持筛选分页 |
| 安全 | 管理员 bcrypt + JWT；首次登录强制改密；私钥 AES-256-GCM 加密存储，任何 HTTP 响应与前端资源均不包含私钥 |

## 快速开始（Docker）

前置：Docker 24+ 与 `docker compose` v2 插件。详细步骤见 [部署手册](docs/部署手册-CentOS9.md)。

```bash
cd unified-license-platform/deploy
sudo ./deploy.sh
```

脚本会：

1. 检查 Docker / Compose 环境；
2. 生成 `deploy/.env`（`ULP_MASTER_KEY`、`ULP_JWT_SECRET`、初始管理员账号与随机密码，权限 600）；
3. 创建 `deploy/data/licenses` 并将属主调整为容器用户 `10001`；
4. 多阶段构建镜像并启动容器；
5. 等待健康检查通过，输出访问地址与初始凭据。

启动后访问 `http://<服务器IP>:7630`，使用脚本输出的初始账号登录，并立即修改密码。

如需放行防火墙：

```bash
firewall-cmd --permanent --add-port=7630/tcp && firewall-cmd --reload
```

停止与查看日志：

```bash
cd deploy && docker compose logs -f
cd deploy && docker compose down
```

## 本地开发

```bash
# 依赖：Go 1.27+
export ULP_MASTER_KEY="$(openssl rand -hex 32)"
export ULP_ADMIN_USER=admin
export ULP_ADMIN_PASSWORD='Admin12345x'
go run ./cmd/ulpd
```

默认监听 `:7630`，数据目录 `./data`。前端为原生 ES Module SPA，通过 `go:embed` 内嵌，无需 Node 构建。

运行测试：

```bash
go vet ./...
go test ./...
```

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `ULP_MASTER_KEY` | 是 | — | 私钥加密主密钥，至少 16 字符（生产建议 64 位 hex） |
| `ULP_JWT_SECRET` | 否 | 由主密钥派生 | JWT 签名密钥，至少 32 字符 |
| `ULP_ADMIN_USER` | 否 | `admin` | 首次启动创建的管理员账号 |
| `ULP_ADMIN_PASSWORD` | 否 | 随机生成 | 初始密码，首次登录必须修改 |
| `ULP_ADDR` | 否 | `:7630` | 监听地址 |
| `ULP_DATA_DIR` | 否 | `./data` | 数据目录（SQLite、授权文件、检查点） |
| `ULP_TOKEN_TTL` | 否 | `12h` | 登录令牌有效期 |
| `LICENSE_MACHINE_CODE` | 否 | 自动计算 | 覆盖本机机器码（容器/离线验签场景） |

> `ULP_MASTER_KEY` 丢失将导致已存私钥无法解密，**必须与数据卷一起备份**。

## HTTP API

所有管理接口（除健康检查与登录）需要 `Authorization: Bearer <token>`。首次登录返回 `must_change_password=true`，此时除 `/api/admin/me` 与 `/api/admin/password` 外的业务接口返回 `403 password_change_required`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/health` | 健康检查（公开） |
| POST | `/api/admin/login` | 管理员登录（公开） |
| GET | `/api/admin/me` | 当前管理员信息 |
| POST | `/api/admin/password` | 修改密码，返回新令牌 |
| GET | `/api/dashboard` | 看板统计 |
| GET/POST | `/api/projects` | 项目列表 / 新建项目（自动生成密钥对） |
| GET/PATCH/DELETE | `/api/projects/{id}` | 项目详情 / 更新 / 删除（有授权时拒绝删除） |
| GET | `/api/projects/{id}/public-key` | 下载公钥 PEM |
| GET/POST | `/api/licenses` | 授权列表（筛选分页）/ 签发授权 |
| GET/DELETE | `/api/licenses/{id}` | 授权详情（含载荷预览）/ 删除记录 |
| GET | `/api/licenses/{id}/download` | 下载 `.lic`（吊销后返回 409） |
| POST | `/api/licenses/{id}/revoke` | 吊销授权 |
| POST | `/api/licenses/{id}/renew` | 续期（生成新授权，原授权标记为 superseded） |
| GET | `/api/keys` | 密钥列表（不含私钥） |
| POST | `/api/keys/rotate` | 轮换项目密钥 |
| GET | `/api/audit-logs` | 审计日志（筛选分页） |

授权文件名：`<项目ID>-<客户>-<licenseID>.lic`，`Content-Disposition` 同时提供 ASCII `filename` 与 `filename*=UTF-8''` 编码。

## 目录结构

```text
unified-license-platform/
├── cmd/
│   ├── ulpd/                 # 平台服务入口
│   └── verify/               # 离线验签 CLI
├── internal/
│   ├── app/                  # 业务编排（项目、签发、吊销、续期、审计）
│   ├── auth/                 # bcrypt、密码策略、HS256 JWT
│   ├── config/               # 环境变量加载与安全校验
│   ├── httpapi/              # chi 路由与 HTTP 处理器
│   ├── license/              # 授权载荷、信封、签名、机器码、回拨检测
│   ├── models/               # 数据模型
│   ├── secure/               # AES-256-GCM 私钥加解密
│   └── store/                # SQLite 存储层
├── web/                      # 内嵌中文管理界面（原生 ES Module SPA）
├── deploy/                   # Dockerfile、docker-compose.yml、deploy.sh
└── docs/                     # 接入规范、文件格式规范、部署手册、验收报告
```

## 离线验签

```bash
ulp-verify -lic demo-客户-xxxx.lic -pub demo-public.pem -product demo
```

输出 JSON 报告；`ok` / `license_grace` 退出码为 0，其余状态码退出码为 2。可追加 `-machine <机器码>` 指定机器码、`-clock <检查点>` 启用时间回拨检测、`-print-machine` 打印本机机器码。

## 文档

- [接入统一授权平台-适配开发规范](docs/接入统一授权平台-适配开发规范.md)
- [统一授权文件格式规范](docs/统一授权文件格式规范.md)
- [部署手册（CentOS 9）](docs/部署手册-CentOS9.md)
- [联调验收报告](docs/联调验收报告.md)

## 安全说明

- 私钥仅以 AES-256-GCM 密文形式存于数据库，不写入仓库、镜像层、HTTP 响应、下载文件或前端静态资源。
- 吊销只对**在线查询平台**生效；离线部署的客户端无法感知吊销，需通过续期/换发或停机处理。
- 生产环境务必使用 HTTPS 反向代理、限制 7630 端口来源，并定期备份 `data/` 与 `deploy/.env`。

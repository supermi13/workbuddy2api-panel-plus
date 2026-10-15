# 飞牛 fnOS 部署说明

两种用法，按需选：

| 方式 | 适合场景 | 产物 |
|---|---|---|
| A. fpk 应用包 | 日常使用，应用中心管理（开始/停止/升级/卸载） | Actions 产出的 `.fpk` |
| B. SSH + compose | 喜欢手动控制，或 fpk 安装失败时排查 | `fnos/docker-compose.yml` + `.env` |

镜像由 CI 自动构建推送：`ghcr.io/supermi13/workbuddy2api-panel-plus`（linux/amd64）。
**ARM 机型暂时不支持**（CI 只构建 amd64 镜像，fpk 的 `platform=x86`），后续补。

---

## A. fpk 应用包安装

1. 下载 fpk（二选一）：
   - Releases 页：<https://github.com/supermi13/workbuddy2api-panel-plus/releases>
     （正式版建议打 `v*` tag 触发构建，版本号干净；手动构建的版本号形如 `1.0.0-<sha>`）
   - 或自己出包：Actions → **Build** → Run workflow → `target` 选 `fpk` →
     勾选 `publish_release` → 从产生的 Release 下载 `wb2api-panel-*-x86.fpk`
2. 飞牛 **应用中心 → 设置/右上角 → 手动安装 → 选择 fpk**，按向导完成。
3. 桌面打开「WorkBuddy2API 面板」（或浏览器访问 `http://飞牛IP:7863/panel/`），
   先在面板内添加工信使账号。

**数据位置**：`@appdata/wb2api-panel`（账号、配置、日志）。升级/重装不丢失；
卸载也只删应用和容器，不删数据——要彻底清除请在文件管理器中手动删该目录。

**改端口**：端口固定 7863（桌面入口与 `service_port` 都写死）。
如 7863 被占：SSH 进去改 `target/app/docker/docker-compose.yaml` 的
`WB2API_PORT` 环境变量或 ports 映射，然后应用中心里停止 → 启动。
注意改后桌面入口仍指向 7863，需手动用新端口访问面板。

## B. SSH + compose 直部署

```bash
mkdir -p /vol1/1000/docker/wb2api-panel && cd /vol1/1000/docker/wb2api-panel
# 把仓库 fnos/ 下的 docker-compose.yml 和 .env.example 传上来
cp .env.example .env          # 按需改端口/身份/拉取策略
docker compose up -d
docker compose logs -f        # 看启动日志
```

- `PULL_POLICY=always` 时每次启动先联网拉镜像；国内直连 ghcr.io 超时的，
  先手动 `docker load` 离线镜像包，再把 `.env` 里改成 `never`
- 容器内以 uid 10001 运行；若报写入失败，把 `PUID/PGID` 改成目录属主的
  `id -u` / `id -g`
- 数据落在本目录 `config/ auths/ data/` 三个子目录

---

## CI 构建说明（Actions → Build）

| 输入 | 产出 |
|---|---|
| `target=image` | 多架构 Docker 镜像推 GHCR（push main 时的默认行为） |
| `target=exe` | `wb2api-windows-amd64.exe` + `wb2api-linux-amd64` + `wb2api-linux-arm64` |
| `target=fpk` | 飞牛应用包 `wb2api-panel-<版本>-x86.fpk` |
| `target=all` | 以上全部 |
| `publish_release` | 把 exe/fpk 发成 Release 附件（打 `v*` tag 时自动发布，无需勾选） |

exe 直跑（无 Docker 场景）：`./wb2api-windows-amd64.exe`，配置默认识别同目录
`config.json`，可用 `-config <路径>` 指定；账号数据目录与面板端口同容器逻辑
（`auths/`、7863）。

## 目录结构

```text
fnos/
├── docker-compose.yml        # 飞牛部署 compose（SSH 直部署 + fpk 内使用，同一份）
├── .env.example
├── fpk/                      # fpk 项目源码（fnpack 打包用）
│   ├── manifest              # appname=wb2api-panel，platform=x86，端口 7863
│   ├── cmd/                  # 生命周期脚本（compose up/down/status 转调）
│   ├── config/               # privilege（package 用户）+ resource（docker-project）
│   ├── wizard/               # 安装/升级/卸载说明
│   ├── app/
│   │   ├── docker/docker-compose.yaml   # CI 组装时从 ../docker-compose.yml 复制
│   │   ├── ui/               # 桌面入口（iframe → http://IP:7863/panel/）
│   │   └── config.example.json
│   ├── ICON.PNG / ICON_256.PNG
└── 飞牛部署说明.md（本文件）
```

## 已知限制

- 仅 x86（amd64 镜像）；ARM 飞牛机型等 CI 补 arm64 镜像 + `platform=arm` fpk
- `os_min_version=1.2.0` 按官方示例值声明，未在最低版本设备上实测；
  如安装失败请先升级 fnOS
- 未在真实 fnOS 设备上跑过完整安装/升级/卸载回归（CI 只保证 fnpack 打包通过），
  首次部署请按上面步骤验证

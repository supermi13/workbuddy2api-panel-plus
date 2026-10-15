# 飞牛 fnOS 部署说明

两种用法，按需选：

| 方式 | 适合场景 | 产物 |
|---|---|---|
| A. fpk 应用包 | 日常使用，应用中心管理（开始/停止/升级/卸载） | Actions 产出的 `-x86.fpk` / `-arm.fpk` |
| B. SSH + compose | 喜欢手动控制，或 fpk 安装失败时排查 | `fnos/docker-compose.yml` + `.env` |
| C. Windows exe | 电脑无 Docker 场景直接跑 | Actions 产出的 `wb2api-windows-amd64.exe` |

镜像由 CI 自动构建推送：`ghcr.io/supermi13/workbuddy2api-panel-plus`
（**linux/amd64 + linux/arm64 多架构**，Docker 按本机架构自动拉取对应镜像）。
fpk 按架构分 **x86 / arm 两个包**，安装时按机型选（见下）。

---

## A. fpk 应用包安装

1. 确认机型架构：SSH 进飞牛跑 `uname -m`——`x86_64` 选 x86 包，`aarch64` 选 arm 包。
2. 下载 fpk（二选一）：
   - Releases 页：<https://github.com/supermi13/workbuddy2api-panel-plus/releases>
     （正式版建议打 `v*` tag 触发构建，版本号干净；手动构建的版本号形如 `1.0.0-<sha>`）
   - 或自己出包：Actions → **Build** → Run workflow → `target` 选 `fpk` →
     勾选 `publish_release` → 从产生的 Release 下载 `wb2api-panel-*-x86.fpk`
     或 `wb2api-panel-*-arm.fpk`
3. 飞牛 **应用中心 → 设置/右上角 → 手动安装 → 选择 fpk**，按向导完成。
4. 桌面打开「WorkBuddy2API 面板」（或浏览器访问 `http://飞牛IP:7863/panel/`），
   先在面板内添加工信使账号。

**数据位置**：`@appdata/wb2api-panel`（账号、配置、日志）。升级/重装不丢失；
卸载也只删应用和容器，不删数据——要彻底清除请在文件管理器中手动删该目录。

**改端口**（默认 7863，被占用时）：
- **推荐**：飞牛 **应用中心 → 本应用 → 设置**，「服务端口」改成 1024-65535 的
  空闲端口 → 保存 → **重启应用**。桌面入口会自动指向新端口。
- 安装时的安装向导里也能直接改（默认值 7863）。
- SSH 方式：编辑 `@appdata/wb2api-panel/port`（一行整数）后重启应用；
  此方式桌面入口不跟随，需手动访问 `http://飞牛IP:<端口>/panel/`。
- 端口被其他进程占用时应用会启动失败，日志会给出明确提示与排查命令
  （`ss -ltnp | grep <端口>`）。

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

## C. Windows exe 直跑（x86 电脑，无 Docker）

1. Releases 页下载 `wb2api-windows-amd64.exe`（或 Actions → `target=exe` 自build）。
2. 放到一个空目录双击运行；配置默认识别同目录 `config.json`
   （没有会自动生成），可用 `-config <路径>` 指定。
3. 浏览器打开 `http://127.0.0.1:7863/panel/`，先在面板内添加工信使账号。
4. 账号数据在 exe 同目录 `auths/`；开机自启可建计划任务或写成 Windows 服务。

---

## CI 构建说明（Actions → Build）

| 输入 | 产出 |
|---|---|
| `target=image` | 多架构 Docker 镜像推 GHCR（linux/amd64 + linux/arm64；push main 时的默认行为） |
| `target=exe` | `wb2api-windows-amd64.exe`（Windows 64 位，x86 电脑直接用） |
| `target=fpk` | 飞牛应用包两个：`wb2api-panel-<版本>-x86.fpk` + `wb2api-panel-<版本>-arm.fpk` |
| `target=all` | 以上全部 |
| `publish_release` | 把 exe/fpk 发成 Release 附件（打 `v*` tag 时自动发布，无需勾选） |

exe 直跑（无 Docker 场景）：`wb2api-windows-amd64.exe`，配置默认识别同目录
`config.json`，可用 `-config <路径>` 指定；账号数据目录与面板端口同容器逻辑
（`auths/`、7863）。

## 目录结构

```text
fnos/
├── docker-compose.yml        # 飞牛部署 compose（SSH 直部署 + fpk 内使用，同一份）
├── .env.example
├── fpk/                      # fpk 项目源码（fnpack 打包用）
│   ├── manifest              # appname=wb2api-panel，platform=x86，端口 7863
│   │                         # （ARM 包由 CI 复制工程并注入 platform=arm 后打包）
│   ├── cmd/                  # 生命周期脚本（compose up/down/status 转调）
│   ├── config/               # privilege（package 用户）+ resource（docker-project）
│   ├── wizard/               # 安装/设置向导（端口可改）/ 升级/卸载说明
│   ├── app/
│   │   ├── docker/docker-compose.yaml   # CI 组装时从 ../docker-compose.yml 复制
│   │   ├── ui/               # 桌面入口（iframe → http://IP:<端口>/panel/）
│   │   └── config.example.json
│   ├── ICON.PNG / ICON_256.PNG
└── 飞牛部署说明.md（本文件）
```

**安装后布局映射**（fnpack 官方规则，排错常查）：项目 `app/` 目录的内容 →
安装后的 `target/`（即 `TRIM_APPDEST`）。所以脚本里 compose 在
`$TRIM_APPDEST/docker/docker-compose.yaml`、桌面入口在
`$TRIM_APPDEST/ui/config`、示例配置在 `$TRIM_APPDEST/config.example.json`，
**都不带 `app/` 前缀**；`manifest/cmd/config/wizard` 装在 `target` 的上级目录
（与 `cmd/` 同级）。数据目录 `TRIM_PKGVAR` = `@appdata/wb2api-panel`，
端口文件就在那里的 `port`。

## 已知限制

- x86 / arm 两个 fpk 需按机型架构选择（`uname -m` 确认），装错架构包飞牛会拒绝安装
- `os_min_version=1.2.0` 按官方示例值声明，未在最低版本设备上实测；
  如安装失败请先升级 fnOS
- 未在真实 fnOS 设备上跑过完整安装/升级/卸载回归（CI 只保证 fnpack 打包通过），
  首次部署请按上面步骤验证

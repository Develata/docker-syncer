# Docker Image Syncer

使用 Go 与 Skopeo 在 Registry 之间直接复制 OCI/Docker 镜像；不拉取到 Docker daemon，也不依赖 Docker。源可以是 Skopeo `docker://` transport 可访问的任意 OCI Distribution Registry；配置内置阿里云 ACR、GHCR 或同时同步两端。

```text
images.txt / CLI / JSON
          │
          ▼
parse → normalize → plan → resolve immutable digest
                              │
                              ▼
                           Skopeo ──→ Aliyun ACR
                              └─────→ GHCR
                                      │
                                      ▼
                                verify → report
```

Go 程序是唯一同步语义所有者；Actions 和本地运行调用同一入口。双目标默认从源各复制一次，不引入临时 OCI 缓存或本地 layer store。

## 快速使用

本地 CLI 需要 Go 1.23+ 与 Skopeo：

```bash
go build -o bin/docker-syncer ./cmd/docker-syncer

# 离线检查解析、命名和 GHCR 计划；不会登录或访问 Registry
GHCR_NAMESPACE=your-github-owner ./bin/docker-syncer sync \
  --config syncer.json --mode ghcr --file images.txt --dry-run=true
```

Windows 建议在 WSL2 中安装并运行 Skopeo，无需 Docker Desktop。

## 配置与优先级

`syncer.json` 是严格 JSON：未知字段、重复字段、`null` 和无效值都会报错。

```json
{
  "mode": "double",
  "platform": "linux/amd64",
  "retries": 2,
  "timeout": "10m"
}
```

优先级为 **CLI 显式参数 > 非空环境变量 > JSON > 默认值**。环境变量：

- `SYNC_MODE`：`aliyun`、`ghcr`、`double` 或 `none`；`none` 只规划，不发布。
- `SYNC_PLATFORM`：`os/arch[/variant]` 或 `all`。
- `ALIYUN_REGISTRY`、`ALIYUN_NAMESPACE`。
- `GHCR_NAMESPACE`；未设置时使用小写的 `GITHUB_REPOSITORY_OWNER`。
- `SYNC_RETRIES`、`SYNC_TIMEOUT`。

`images.txt` 每行一个源镜像，可附加 `--platform` 与 `--target`，详见 `images.example.txt`。`linux/arm64` 规范化为 `linux/arm64/v8`；`linux/arm` 必须明确 `/v6` 或 `/v7`。

未写 `--target` 时，目标名是源镜像规范化全引用的 slug（最长 50 字符）、`--`、12 位哈希与源 tag。哈希输入精确为：

```text
<canonical full reference>\n<normalized platform>
```

因此平台稳定参与命名，且不同 registry/namespace 不会再依赖易碰撞的全局“路径压平”算法。显式 `--target repository:tag` 不允许斜杠，可用于保留旧名称；同一批计划若把不同源或平台指向同一目标会在任何网络 I/O 前失败。仓库自带的 `images.txt` 已为原有 10 个镜像逐项声明旧目标名，迁移不会静默改名。

## 一致性语义

- **单平台**：读取所选 descriptor 对应 child manifest 的 raw bytes，计算 SHA-256；复制时使用固定的 `source@sha256:...` 和 `--preserve-digests`。
- **`platform=all`**：对 raw manifest list/index 计算 SHA-256，固定该 index digest，并使用 `--all --preserve-digests`；结果不取决于 runner 架构。
- 发布后重新读取目标 raw manifest 并校验哈希；目标验证失败即整个命令非零退出。
- `Created` 字段只是元数据，永不作为镜像身份或“相同”的依据。

`--force=true` 跳过发布前的目标一致性检查，但不跳过复制后的验证。任一条目或任一目标失败，命令最终非零退出；详细结果写入 `--summary` 指定的 Markdown。

当前有意只接受 SHA-256 digest，并顺序处理镜像以保持日志稳定、控制 Registry 压力与内存。若目标 Registry 不能在 `--preserve-digests` 下保存原 manifest，任务会显式失败而不会把“近似相同”报告为成功；单平台遇到嵌套 index 也会要求改用 `all`，而非猜测平台。

## Dry run

```bash
./bin/docker-syncer sync --config syncer.json --file images.txt --dry-run=true
```

Dry run **仅做离线解析、规范化、碰撞检查与计划输出**：不读取 Registry、不登录、不验证凭据、不复制。它不能证明网络、凭据或 Registry 权限有效。

## 认证

程序默认接收一个明确的 Skopeo authfile，不隐式回退到 Docker/containers 的全局凭据。建议每次运行创建隔离文件：

```bash
authfile=$(mktemp)
chmod 600 "$authfile"
trap 'rm -f "$authfile"' EXIT
printf '{}\n' >"$authfile"
printf '%s' "$GHCR_TOKEN" | skopeo login \
  --authfile "$authfile" --username "$GHCR_USER" \
  --password-stdin ghcr.io
GHCR_NAMESPACE=your-github-owner ./bin/docker-syncer sync \
  --config syncer.json --mode ghcr --file images.txt --authfile "$authfile"
```

GitHub Actions 保留以下 Secrets：

| Secret | 用途 |
|---|---|
| `ALIYUN_REGISTRY` / `ALIYUN_NAMESPACE` | ACR 目标 |
| `ALIYUN_USERNAME` / `ALIYUN_PASSWORD` | ACR 登录 |
| `DOCKERHUB_USERNAME` / `DOCKERHUB_TOKEN` | 可选 Docker Hub 源登录 |
| `GITHUB_TOKEN` | GHCR 源/目标登录（Actions 自动提供） |
| `WEBHOOK_URL` | 可选飞书文本通知 |

`scripts/ci-sync.sh` 只负责建立权限为 0600 的临时 authfile、按解析后的 mode 登录、原样转发输入和发送有超时的通知；业务判断全部由 Go CLI 完成。认证错误不会输出 Registry stderr，通知失败只产生 warning，且不会掩盖同步退出码。

## GitHub Actions

- `Sync images`：在每月 1、11、21 日 UTC 00:00 定时运行；`main` 的 `images.txt`/`syncer.json` 变化也会触发。手动运行时 `image_name` 留空即批量；可覆盖 `mode`、`platform`、`target_name`、`force_sync`、`dry_run`。
- `Check`：PR 与 `main` push 上执行 fmt、test、vet、race、Shell/workflow lint，以及使用 Skopeo 和本地 registry 包的 integration tests；不读取 Secrets。
- 发布工作流只有 `packages: write`，全局默认仅 `contents: read`；checkout 不持久化凭据。发布并发跨触发源串行，运行中的发布不会被取消。
- Dependabot 每月检查 GitHub Actions 与 Go modules。

旧的四个工作流已合并；`clean_disk` 与 `new_name` 输入、`config.env`、Dockerfile/Compose 和两套重复本地脚本均已删除。兼容的重命名入口为 `target_name`；批量列表使用 `--target`。

## License

MIT License。

# 🐳 Docker Image Syncer (镜像搬运工)

利用 GitHub Actions 的免费算力和高速网络，将 DockerHub 的镜像搬运到 **阿里云容器镜像服务 (ACR)** 或 **GitHub Packages (GHCR)**。

**✨ 核心优势 (Pro版):**

1.  **多架构支持**：支持指定 `--platform` (如 `linux/arm64`)，完美适配树莓派、M1/M2 Mac 等设备。
2.  **架构感知智能同步**：
    *   **解决官方镜像误判**：脚本会精准比对**特定架构**的摘要 (Digest)，真正实现“内容不变则跳过”。
    *   **双重校验机制**：不仅比对摘要 (Digest)，还会比对镜像创建时间 (Created)。防止因 Registry 压缩算法不同导致的死循环推送。
3.  **极致空间优化**：
    *   支持**磁盘清理开关**，防止 Runner 空间不足。
    *   **即下即删**：推送完一个镜像立即清理，支持大批量同步。
4.  **智能命名策略**：
    *   自动处理命名空间冲突（如 `bitnami/nginx` -> `bitnami_nginx`）。
    *   自动适配 GHCR 的小写命名要求。

---

![Last Sync](https://img.shields.io/badge/last%20sync-2026--04--11%2009:50:22-green)

## ⚡ 快速开始 (Quick Start)

1.  点击仓库右上角的 **[Use this template]** 按钮，选择 **Create a new repository**。
2.  建议设为 **Public** (Public 仓库使用 GHCR 存储空间无限制，且拉取免鉴权)。
3.  按照下方 [配置 Secrets](#%EF%B8%8F-%E9%85%8D%E7%BD%AE-secrets) 完成设置。
4.  **修改配置**：根据需要修改 `config.env` 文件（设置同步模式等）。

---

## 🛠️ 配置说明 (Configuration)

### 1. Secrets 设置
进入仓库的 **Settings** -> **Secrets and variables** -> **Actions** -> **New repository secret**：

| Secret Name | 必填 | 说明 | 示例 |
| --- | --- | --- | --- |
| `ALIYUN_REGISTRY` | ✅ | 阿里云 ACR 公网地址 | `registry.cn-hangzhou.aliyuncs.com` |
| `ALIYUN_NAMESPACE` | ✅ | 你的命名空间 | `develata` |
| `ALIYUN_USERNAME` | ✅ | 阿里云登录账号 | `username@123.onaliyun.com` |
| `ALIYUN_PASSWORD` | ✅ | 阿里云 **访问凭证密码** | `Mypassword123` |
| `DOCKERHUB_USERNAME` | ❌ | DockerHub 用户名 (防限流) | `myuser` |
| `DOCKERHUB_TOKEN` | ❌ | DockerHub Token (推荐) | `dckr_pat_xxx` |
| `WEBHOOK_URL` | ❌ | 消息通知 Webhook | `https://open.feishu.cn/...` |

### 2. 配置文件 (`config.env`)
项目根目录下的 `config.env` 控制全局行为：

```bash
# 同步模式: aliyun, ghcr, double, none
SYNC_MODE=aliyun

# 磁盘清理: true (默认) / false
# 开启可释放约 2GB 空间，但会增加 2-4 分钟运行时间
CLEAN_DISK_SPACE=true
```

### 3. Webhook 配置指南
以**飞书 (Feishu)** 为例：
1.  在飞书群组中添加“自定义机器人”。
2.  **不需要**在飞书设置特定的“关键字”或“IP白名单”（如果脚本报错，检查是否被拦截，通常只需获取 URL）。
3.  复制 Webhook URL (格式如 `https://open.feishu.cn/open-apis/bot/v2/hook/xxx`)。
4.  填入 GitHub Secrets 的 `WEBHOOK_URL` 中。
*本程序已内置 JSON 消息格式，无需用户在飞书端配置消息模板。*

---

## 🚀 工作流选择 Guide

本项目包含 **3 类** 工作流，请根据需求选择：

### 1. 自动批量同步 (`01. Batch Sync`)
*   **适用场景**：日常大批量同步，无人值守。
*   **触发方式**：
    *   修改 `images.txt` 推送。
    *   定时触发 (Schedule)。
    *   手动触发 (Workflow Dispatch)。
*   **配置**：
    *   **镜像列表**：读取根目录 `images.txt`。
    *   **同步目标**：完全由 `config.env` 中的 `SYNC_MODE` 决定。
    *   **磁盘清理**：由 `config.env` 中的 `CLEAN_DISK_SPACE` 决定。

**`images.txt` 示例** (更多详见 `images.example.txt`)：
```text
nginx:alpine
--platform=linux/arm64 mysql:8.0
bitnami/redis:latest
```

**⚠️ 重要：参数使用规范**
1. **推荐格式**：参数写在镜像名**后面**
   ```text
   # ✅ 推荐 - 镜像名在前，参数在后
   mysql:8.0 --platform=linux/arm64
   nginx:alpine --disable-content-trust
   redis:7.2 --platform=linux/arm64 --pull-always
   ```

2. **支持格式**：参数也可以写在镜像名**前面**（已优化支持）
   ```text
   # ✅ 支持 - 参数在前（但不推荐）
   --platform=linux/arm64 mysql:8.0
   --disable-content-trust nginx:alpine
   ```

3. **参数格式类型**：
   - **带值参数**（等号格式）：`--platform=linux/arm64`
   - **带值参数**（空格格式）：`--platform linux/arm64`
   - **无值参数**（布尔标志）：`--disable-content-trust` `--pull-always` `--no-cache`

4. **不支持的写法**：
   ```text
   # ❌ 错误 - 多个镜像写在一行
   nginx:alpine redis:7.2
   
   # ❌ 错误 - 参数值包含空格但未加引号
   --label description=my app
   ```

5. **最佳实践**：
   - 每行只写一个镜像
   - 镜像名始终在前，参数在后
   - 仅使用脚本明确支持的参数（主要是 `--platform`）
   - 其他 docker 参数（如 `--disable-content-trust`）会被自动过滤，不影响同步

### 2. 双重同步 (`02. Double Sync`)
*   **适用场景**：临时手动同步一个镜像，同时推送到 阿里云 和 GHCR。
*   **特点**：三方比对（源 vs 阿里 vs GHCR），智能判断哪一方需要更新。
*   **手动参数**：
    *   `image_name`: 原镜像名。
    *   `target_name`: (可选) 重命名。
    *   `clean_disk`: 是否清理磁盘 (默认 False 以节省时间)。

### 3. 单项同步 (`03. To Aliyun` / `04. To GHCR`)
*   **适用场景**：只推送到某一个特定的仓库。
*   **特点**：**不读取** `images.txt`，仅处理本次输入的镜像。
*   **独立性**：你可以用它来临时搬运一个不在列表里的镜像。

---

## ❓ 常见问题 (FAQ)

### Q1: 关于磁盘清理 (Disk Space)
**现象**：Workflow 开头经常要跑几分钟 "Maximize Build Space"。
**解释**：GitHub Runner 默认磁盘空间有限（约 14GB 可用）。如果同步大镜像（如 PyTorch, TensorFlow），很容易爆盘。脚本会预先移除 .NET, Android 等组件释放空间。
**调整**：
*   如果只是同步 nginx 这种小镜像，可以在 `config.env` 把 `CLEAN_DISK_SPACE` 设为 `false`。
*   手动同步时，取消勾选 `clean_disk` 选项。

### Q2: 关于架构指定 (/v8 问题)
**问**：镜像名是 `linux/arm64/v8`，我该怎么写？
**答**：只需要写 `--platform=linux/arm64` 即可。Docker 引擎会自动协商最匹配的变体 (Variant)。不需要显式写 `/v8`，否则可能会报错。

### Q3: 为什么有些参数被忽略了？
**问**：我在 `images.txt` 中写了 `nginx:latest --disable-content-trust`，但参数好像没生效？
**答**：这是正常的。脚本会自动**过滤掉所有参数**，只提取镜像名。目前只有 `--platform` 参数会被解析和使用。其他参数（如 `--disable-content-trust`）会被安全地移除，不会影响同步过程。

### Q4: images.txt 中参数的正确写法
**最佳实践**：
```text
# ✅ 推荐 - 镜像名在前
mysql:8.0 --platform=linux/arm64
nginx:alpine

# ✅ 也支持 - 参数在前（已优化）
--platform=linux/arm64 mysql:8.0

# ❌ 避免 - 复杂的参数组合
mysql:8.0 --platform linux/arm64 --pull-always --no-cache
```
**说明**：虽然脚本已优化支持参数在前后的各种情况，但为了提高可读性，建议统一使用"镜像名在前，参数在后"的格式。

### Q5: 为什么 GHCR 403 Forbidden?
1.  确保在 GitHub 个人设置 -> Developer Settings 开启了 `write:packages` 权限（如果是用 Token）。
2.  本 Actions 默认使用 `${{ secrets.GITHUB_TOKEN }}`，请检查仓库 Settings -> Actions -> General -> **Workflow permissions** 必须设为 **Read and write permissions**。

---

## 📜 License

MIT License.

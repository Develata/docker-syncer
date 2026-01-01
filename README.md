# 🐳 Docker Image Syncer (镜像搬运工)

利用 GitHub Actions 的免费算力和高速网络，将 DockerHub 的镜像搬运到 **阿里云容器镜像服务 (ACR)** 或 **GitHub Packages (GHCR)**。

**✨ 核心优势 (Pro版):**

1. **多架构支持**：支持指定 `--platform` (如 `linux/arm64`)，完美适配树莓派、M1/M2 Mac 等设备。
2. **架构感知智能同步**：
*
* 解决官方镜像（如 `mysql:8.0`）是 Manifest List 导致的误判问题。脚本会精准比对**特定架构**的摘要 (Digest)，真正实现“内容不变则跳过”。
* **双重校验机制**：不仅比对摘要 (Digest)，还会比对镜像创建时间 (Created)。即使 Registry 可能会修改镜像压缩方式导致摘要改变，只要创建时间一致，依然判定为同源，防止死循环重复推送。


3. **极致空间优化**：
* 构建前自动清理 .NET/Android 等 10GB+ 无用环境。
* **即下即删**：推送完一个镜像立即清理，支持大批量、大体积镜像同步，防止 Runner 磁盘爆满。


4. **智能命名策略**：
* 自动处理命名空间冲突（如 `bitnami/nginx` -> `bitnami_nginx`）。
* 自动适配 GHCR 的小写命名要求。



---

![Last Sync](https://img.shields.io/badge/last%20sync-2026--01--02%2005:04:08-green)

## ⚡ 快速开始 (Quick Start)

1. 点击仓库右上角的 **[Use this template]** 按钮，选择 **Create a new repository**。
2. 输入仓库名称，建议设为 **Public** (Public 仓库使用 GHCR 存储空间无限制)。
3. 按照下方 [配置 Secrets](https://www.google.com/search?q=%23%EF%B8%8F-%E9%85%8D%E7%BD%AE-secrets) 完成设置。
4. 自动操作（见使用指南）或手动操作步骤：
  * 点击 Actions 页面 -> 选择对应工作流。
  * 点击 **Run workflow**。
  * 输入参数：
     * `image_name`: 原镜像名 (如 `gitea/gitea:1.20`)
     * `new_name` (仅Aliyun) / `target_name` (仅GHCR): (可选) 重命名目标镜像
     * `platform`: (可选) 指定架构
     * `force_sync`: (可选) 强制同步
---

## 🛠️ 配置 Secrets

进入仓库的 **Settings** -> **Secrets and variables** -> **Actions** -> **New repository secret**：

| Secret Name | 必填 | 说明 | 示例 |
| --- | --- | --- | --- |
| `ALIYUN_REGISTRY` | ✅ | 阿里云 ACR 公网地址 | `registry.cn-hangzhou.aliyuncs.com` |
| `ALIYUN_NAMESPACE` | ✅ | 你的命名空间 | `develata` |
| `ALIYUN_USERNAME` | ✅ | 阿里云登录账号 | `username@123.onaliyun.com` |
| `ALIYUN_PASSWORD` | ✅ | 阿里云 **访问凭证密码** | `Mypassword123` |
| `DOCKERHUB_USERNAME` | ❌ | DockerHub 用户名 (推荐配置，防限流) | `myuser` |
| `DOCKERHUB_TOKEN` | ❌ | DockerHub Token (推荐配置) | `dckr_pat_xxx` |
| `WEBHOOK_URL` | ❌ | 消息通知 webhook 地址 (飞书/钉钉) | `https://open.feishu.cn/...` |

> **提示**：GHCR 无需配置 Secret，脚本默认使用 `${{ secrets.GITHUB_TOKEN }}` 鉴权。

### 🔔 消息通知 (Notifications)

如果配置了 `WEBHOOK_URL`，任务结束时（成功或失败）会发送 JSON 通知。
支持 **飞书 (Feishu/Lark)**、**钉钉 (DingTalk)** 等平台。

---

## 🚀 使用指南 (Usage)

本项目提供 **4 种** 强大的工作流，满足不同场景需求：

### 1. 自动批量同步 (`sync-batch-daily.yml`)

适合定期同步大量镜像。

* **触发方式**：
  * **Push 触发**：修改并提交 `images.txt` 文件时自动运行。
  * **定时触发**：每天/每周定时运行。
  * **手动触发**：点击 Run workflow 手动运行。
* **手动触发参数**：
  * `force_sync`: (可选) 勾选则跳过摘要比对，强制同步所有镜像。
* **配置方式**：
  * **镜像列表**：修改仓库根目录的 `images.txt`。
  * **同步配置**：修改仓库根目录的 `config.env`。
    * `SYNC_MODE`: 默认同步模式 (可选 `aliyun`, `ghcr`, `double`, `none`, 默认为 `none` 防止误触)。

**`images.txt` 语法示例：**

```text
# 基础镜像 (默认 amd64)
nginx:alpine

# 指定架构 (同步 arm64 版本)
--platform=linux/arm64 mysql:8.0

# 包含命名空间的镜像 (自动转存为 bitnami_redis:latest)
bitnami/redis:latest

# 支持空行和 # 注释

```

### 2. 双重同步 (`02. 双重同步`)

**最强模式**。同时检查阿里云和 GHCR。

* **逻辑**：源 vs 阿里 vs GHCR 三方比对。
* **效果**：如果阿里云有了但 GHCR 没有，只推 GHCR；全都有则全跳过。
* **输入参数**：
* `image_name`: 原镜像名 (如 `python:3.9`)
* `target_name`: (可选) 自定义目标名称 (如 `my-python`)
* `platform`: (可选) 如 `linux/arm64`
* `force_sync`: (可选) 勾选则强制覆盖



### 3. 单向同步 (`03. 单项同步` / `04. 单项同步`)

适合临时搬运单个镜像到指定仓库。

* **操作步骤**：
  1. 点击 Actions 页面 -> 选择对应工作流。
  2. 点击 **Run workflow**。
  3. 输入参数：
     * `image_name`: 原镜像名 (如 `gitea/gitea:1.20`)
     * `new_name` (仅Aliyun) / `target_name` (仅GHCR): (可选) 重命名目标镜像
     * `platform`: (可选) 指定架构
     * `force_sync`: (可选) 强制同步



---

## 📥 拉取镜像 (Pull)

同步完成后，查看 Actions 日志获取拉取地址，或参考以下规则：

#### 默认情况 (amd64)

```bash
# 原图: nginx:latest
docker pull registry.cn-hangzhou.aliyuncs.com/<你的命名空间>/nginx:latest

```

#### 命名空间处理

```bash
# 原图: bitnami/redis:latest
docker pull registry.cn-hangzhou.aliyuncs.com/<你的命名空间>/bitnami_redis:latest

```

#### 多架构处理 (--platform=linux/arm64)

```bash
# 原图: mysql:8.0 (指定 arm64)
# 脚本会自动加上架构前缀，防止覆盖
docker pull registry.cn-hangzhou.aliyuncs.com/<你的命名空间>/linux_arm64_mysql:8.0

```

---

## ❓ 常见问题 (FAQ)

### Q: 为什么要指定 `--platform`？

**A**:

1. **需求层面**：如果你是用 M1/M2 Mac 或者树莓派，必须拉取 `arm64` 架构的镜像才能运行。
2. **技术层面**：官方镜像（如 MySQL）通常是一个列表。如果不指定架构，Skopeo 获取的是列表摘要，而 Docker 拉取的是具体镜像摘要，两者不一致会导致脚本认为“镜像有更新”，从而无法跳过，浪费资源。

### Q: GHCR 为什么拉取失败？

**A**:

1. 确保你在 GitHub 个人设置中打开了 Packages 权限。
2. 如果仓库是 **Private** 的，你需要用 Token 登录 Docker 才能拉取。**建议将本仓库设为 Public**，这样生成的包也是 Public 的，可以免登录拉取。

---

## 📜 License

MIT License. 仅供个人学习与技术交流使用。

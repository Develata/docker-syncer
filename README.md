# 🐳 Docker Image Syncer (镜像搬运工)

![GitHub Actions](https://img.shields.io/badge/GitHub_Actions-2088FF?style=for-the-badge&logo=github-actions&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-2496ED?style=for-the-badge&logo=docker&logoColor=white)
![Aliyun](https://img.shields.io/badge/Aliyun_ACR-FF6A00?style=for-the-badge&logo=alibabacloud&logoColor=white)

利用 GitHub Actions 的免费算力和高速网络，将 DockerHub 的镜像搬运到 **阿里云容器镜像服务 (ACR)** 或 **GitHub Packages (GHCR)**。

**✨ 核心优势：**
1.  **解决拉取慢**：通过阿里云国内节点，实现服务器/本地满速拉取。
2.  **无需服务器**：完全依赖 GitHub Actions，无需消耗自己的 VPS 资源。
3.  **按需触发**：手动输入镜像名，想搬什么搬什么，无需维护复杂的同步列表。

---

## 🛠️ 准备工作 (Setup)

在使用之前，你需要配置 GitHub 仓库的 **Secrets**。

1. 进入仓库的 **Settings** -> **Secrets and variables** -> **Actions**。
2. 点击 **New repository secret**，添加以下变量：

| Secret Name | 说明 (Value) | 示例 |
| :--- | :--- | :--- |
| `ALIYUN_USERNAME` | 阿里云 ACR 登录账号 | `username@123456.onaliyun.com` |
| `ALIYUN_PASSWORD` | 阿里云 ACR **访问凭证密码** (非登录密码) | `Mypassword123` |
| `ALIYUN_REGISTRY` | 阿里云 ACR 公网地址 | `registry.cn-hangzhou.aliyuncs.com` |
| `ALIYUN_NAMESPACE` | 你的命名空间 | `develata` |
| `DOCKERHUB_USERNAME` | (可选) DockerHub 用户名，防限流 | `myuser` |
| `DOCKERHUB_TOKEN` | (可选) DockerHub Token/密码 | `dckr_pat_xxx` |

> **注意**：GHCR (GitHub Packages) 无需配置 Secret，脚本默认使用 `${{ secrets.GITHUB_TOKEN }}` 进行鉴权。

---

## 🚀 如何使用 (Usage)

### 1. 启动搬运
1. 点击仓库顶部的 **Actions** 标签页。
2. 在左侧选择对应的工作流（例如 **"镜像搬运工"**）。
3. 点击右侧的 **Run workflow** 按钮。
4. 输入参数：
    * **原镜像名称**: `mysql:8.0` (必填)
    * **保存名称**: 留空则默认与原名一致 (选填)
5. 点击绿色 **Run workflow** 按钮。

### 2. 等待完成
通常只需要 30秒 - 1分钟。等待图标变成 ✅ 绿色。

### 3. 拉取镜像
点击运行记录，查看 **Build** 日志，或者直接拼接地址拉取：

**从阿里云拉取 (推荐国内使用):**
```bash
# 首次需登录
docker login --username=<你的阿里云用户名> registry.cn-hangzhou.aliyuncs.com

# 拉取
docker pull [registry.cn-hangzhou.aliyuncs.com/](https://registry.cn-hangzhou.aliyuncs.com/)<你的命名空间>/mysql:8.0

```

**从 GHCR 拉取:**

```bash
docker pull ghcr.io/<你的GitHub用户名>/mysql:8.0

```

---

## 📝 工作流介绍

本项目包含以下 GitHub Actions Workflow：

* **Sync to Aliyun (`sync-to-aliyun.yml`)**:
* 专门将镜像推送到阿里云个人版 ACR。
* **适用场景**：国内服务器生产环境使用，速度极快。


* **Sync to GHCR (`mirror-to-ghcr.yml`)**:
* 专门将镜像推送到 GitHub Packages。
* **适用场景**：海外服务器使用，或作为冷备份。



---

## ⚠️ 注意事项

1. **GHCR 存储额度**：
* 如果本仓库是 **Private (私有)**，生成的 GHCR 包默认也是私有的，会占用免费用户的 **500MB** 存储额度。
* **建议**：将本仓库设为 **Public (公开)**，或者在推送后手动将 Package 的属性改为 Public，以获取无限存储空间。


2. **阿里云 ACR 配置**：
* 建议将阿里云命名空间设为 **"私有"**，防止他人随意拉取你的镜像（除非你做公益镜像站）。
* 私有仓库拉取前必须执行 `docker login`。


3. **命名规范**：
* GHCR 强制要求镜像名**全小写**。脚本中已包含自动转换逻辑，无需担心。



---

## 📜 License

MIT License. 仅供个人学习与技术交流使用。

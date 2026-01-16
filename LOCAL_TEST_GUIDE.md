# Docker Syncer 本地测试指南

## 快速开始

### 1. 配置环境变量

编辑 `.env.local` 文件，填写你的配置信息：

```bash
# 阿里云配置
ALIYUN_REGISTRY=registry.cn-hangzhou.aliyuncs.com
ALIYUN_NAMESPACE=your-namespace
ALIYUN_USERNAME=your-username
ALIYUN_PASSWORD=your-password

# GitHub 配置（用于 GHCR）
GITHUB_REPOSITORY_OWNER=your-github-username
GITHUB_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxx
```

### 2. 运行测试脚本

```bash
# 给脚本添加执行权限（Linux/Mac）
chmod +x local-test.sh

# 测试单个镜像同步到阿里云
bash local-test.sh --single nginx:alpine --aliyun

# 测试单个镜像同步到 GHCR
bash local-test.sh --single nginx:alpine --ghcr

# 测试双重同步（阿里云 + GHCR）
bash local-test.sh --single nginx:alpine --double

# 模拟运行（不实际推送）
bash local-test.sh --single nginx:alpine --dry-run
```

### 3. Windows PowerShell 测试

**推荐方式：使用 PowerShell 脚本**

```powershell
# 测试单个镜像同步到阿里云
.\local-test.ps1 -Mode aliyun -Image nginx:alpine

# 测试单个镜像同步到 GHCR
.\local-test.ps1 -Mode ghcr -Image nginx:alpine

# 测试双重同步（阿里云 + GHCR）
.\local-test.ps1 -Mode double -Image nginx:alpine

# 模拟运行（不实际推送）
.\local-test.ps1 -Image nginx:alpine -DryRun

# 查看帮助
.\local-test.ps1 -Help
```

**手动方式：逐步执行**

如果需要手动测试，可以：

```powershell
# 加载环境变量
Get-Content .env.local | ForEach-Object {
    if ($_ -match '^([^#=]+)=(.*)$') {
        [System.Environment]::SetEnvironmentVariable($matches[1], $matches[2], "Process")
    }
}

# 登录阿里云
$env:ALIYUN_PASSWORD | docker login $env:ALIYUN_REGISTRY -u $env:ALIYUN_USERNAME --password-stdin

# 测试同步
docker pull nginx:alpine
docker tag nginx:alpine "$env:ALIYUN_REGISTRY/$env:ALIYUN_NAMESPACE/nginx:alpine"
docker push "$env:ALIYUN_REGISTRY/$env:ALIYUN_NAMESPACE/nginx:alpine"
```

## 测试场景

### 场景 1: 测试带架构的镜像

```bash
# ARM64 架构
bash local-test.sh --single mysql:8.0 --platform linux/arm64 --aliyun

# 注意：目前脚本还不支持 --platform 参数，需要手动指定
docker pull --platform linux/arm64 mysql:8.0
```

### 场景 2: 测试私有 Registry

```bash
# 测试带端口号的私有 Registry
bash local-test.sh --single 192.168.1.100:5000/my-app:latest --aliyun
```

### 场景 3: 测试命名空间

```bash
# 测试带命名空间的镜像
bash local-test.sh --single bitnami/redis:7.2 --double
```

## 调试技巧

### 1. 启用调试模式

在脚本开头添加：
```bash
set -x  # 显示所有执行的命令
```

### 2. 查看镜像信息

```bash
# 使用 skopeo 检查镜像摘要
skopeo inspect docker://nginx:alpine

# 检查特定架构的摘要
skopeo inspect --override-arch arm64 docker://mysql:8.0
```

### 3. 测试 Skopeo 命令

```bash
# 直接使用 skopeo 同步（无需 docker pull）
skopeo copy \
  docker://nginx:alpine \
  docker://$ALIYUN_REGISTRY/$ALIYUN_NAMESPACE/nginx:alpine
```

## 常见问题

### Q1: 权限被拒绝

```bash
# Linux/Mac: 给脚本添加执行权限
chmod +x local-test.sh
```

### Q2: Docker 未登录

确保先运行登录命令：
```bash
docker login $ALIYUN_REGISTRY -u $ALIYUN_USERNAME
```

### Q3: 环境变量未加载

```bash
# 手动加载环境变量
source .env.local

# 验证环境变量
echo $ALIYUN_REGISTRY
```

## 文件说明

- `.env.local` - 本地环境变量配置（已在 .gitignore 中排除）
- `local-test.sh` - 本地测试脚本
- `LOCAL_TEST_GUIDE.md` - 本文档

## 注意事项

1. **不要提交** `.env.local` 文件到 Git 仓库
2. 测试完成后建议**清理本地镜像**以节省空间
3. 使用 `--dry-run` 模式可以安全地测试而不实际推送
4. 阿里云密码是**访问凭证密码**，不是登录密码

## 进阶用法

### 批量测试

创建测试镜像列表 `test-images.txt`：
```text
nginx:alpine
redis:7.2
mysql:8.0
```

然后批量测试（需要自己实现循环）：
```bash
while read image; do
    bash local-test.sh --single "$image" --aliyun
done < test-images.txt
```

### 性能测试

使用 `time` 命令测量同步耗时：
```bash
time bash local-test.sh --single nginx:alpine --double
```

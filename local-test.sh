#!/bin/bash
# ========================================
# Docker Syncer 本地测试脚本
# Local Test Script for Docker Syncer
# ========================================
#
# 使用方法：
# 1. 填写 .env.local 中的配置
# 2. 运行: bash local-test.sh [选项]
#
# 选项：
#   --single IMAGE     测试单个镜像同步
#   --batch            测试批量同步（使用 images.txt）
#   --aliyun           只同步到阿里云
#   --ghcr             只同步到 GHCR
#   --double           双重同步（阿里云 + GHCR）
#   --dry-run          模拟运行，不实际推送
#

set -e  # 遇到错误立即退出

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 加载环境变量
log_info "加载环境变量..."
if [ -f ".env.local" ]; then
    source .env.local
    log_success "环境变量已加载"
else
    log_error ".env.local 文件不存在！请先创建并配置该文件。"
    log_info "提示: 可以复制 .env.local 模板文件进行配置"
    exit 1
fi

# 检查必需的环境变量
check_required_vars() {
    local mode=$1
    local missing_vars=()
    
    if [[ "$mode" == "aliyun" || "$mode" == "double" ]]; then
        [ -z "$ALIYUN_REGISTRY" ] && missing_vars+=("ALIYUN_REGISTRY")
        [ -z "$ALIYUN_NAMESPACE" ] && missing_vars+=("ALIYUN_NAMESPACE")
        [ -z "$ALIYUN_USERNAME" ] && missing_vars+=("ALIYUN_USERNAME")
        [ -z "$ALIYUN_PASSWORD" ] && missing_vars+=("ALIYUN_PASSWORD")
    fi
    
    if [[ "$mode" == "ghcr" || "$mode" == "double" ]]; then
        [ -z "$GITHUB_REPOSITORY_OWNER" ] && missing_vars+=("GITHUB_REPOSITORY_OWNER")
        [ -z "$GITHUB_TOKEN" ] && missing_vars+=("GITHUB_TOKEN")
    fi
    
    if [ ${#missing_vars[@]} -ne 0 ]; then
        log_error "缺少必需的环境变量："
        for var in "${missing_vars[@]}"; do
            echo "  - $var"
        done
        exit 1
    fi
}

# 登录 Docker Registry
docker_login() {
    local mode=$1
    
    if [[ "$mode" == "aliyun" || "$mode" == "double" ]]; then
        log_info "登录阿里云 ACR..."
        echo "$ALIYUN_PASSWORD" | docker login "$ALIYUN_REGISTRY" \
            --username "$ALIYUN_USERNAME" --password-stdin
        log_success "阿里云 ACR 登录成功"
    fi
    
    if [[ "$mode" == "ghcr" || "$mode" == "double" ]]; then
        log_info "登录 GitHub Container Registry..."
        echo "$GITHUB_TOKEN" | docker login ghcr.io \
            --username "$GITHUB_REPOSITORY_OWNER" --password-stdin
        log_success "GHCR 登录成功"
    fi
    
    # 可选：登录 Docker Hub（避免限流）
    if [ -n "$DOCKERHUB_USERNAME" ] && [ -n "$DOCKERHUB_TOKEN" ]; then
        log_info "登录 Docker Hub..."
        echo "$DOCKERHUB_TOKEN" | docker login \
            --username "$DOCKERHUB_USERNAME" --password-stdin
        log_success "Docker Hub 登录成功"
    fi
}

# 同步单个镜像
sync_single_image() {
    local src_image=$1
    local mode=$2
    local dry_run=$3
    
    log_info "开始同步镜像: $src_image"
    log_info "同步模式: $mode"
    
    # 提取镜像名（简化处理）
    local image_name=$(basename "$src_image" | sed 's/:.*$//')
    local image_tag=$(echo "$src_image" | grep -oP '(?<=:)[^:]+$' || echo "latest")
    
    log_info "拉取源镜像..."
    if [ "$dry_run" == "true" ]; then
        log_warning "[DRY RUN] docker pull $src_image"
    else
        docker pull "$src_image"
    fi
    
    # 同步到阿里云
    if [[ "$mode" == "aliyun" || "$mode" == "double" ]]; then
        local ali_target="${ALIYUN_REGISTRY}/${ALIYUN_NAMESPACE}/${image_name}:${image_tag}"
        log_info "推送到阿里云: $ali_target"
        
        if [ "$dry_run" == "true" ]; then
            log_warning "[DRY RUN] docker tag $src_image $ali_target"
            log_warning "[DRY RUN] docker push $ali_target"
        else
            docker tag "$src_image" "$ali_target"
            docker push "$ali_target"
            log_success "阿里云推送完成"
        fi
    fi
    
    # 同步到 GHCR
    if [[ "$mode" == "ghcr" || "$mode" == "double" ]]; then
        local ghcr_target="ghcr.io/${GITHUB_REPOSITORY_OWNER}/${image_name}:${image_tag}"
        ghcr_target=$(echo "$ghcr_target" | tr '[:upper:]' '[:lower:]')  # GHCR 要求小写
        log_info "推送到 GHCR: $ghcr_target"
        
        if [ "$dry_run" == "true" ]; then
            log_warning "[DRY RUN] docker tag $src_image $ghcr_target"
            log_warning "[DRY RUN] docker push $ghcr_target"
        else
            docker tag "$src_image" "$ghcr_target"
            docker push "$ghcr_target"
            log_success "GHCR 推送完成"
        fi
    fi
    
    # 清理本地镜像
    if [ "$dry_run" != "true" ]; then
        log_info "清理本地镜像..."
        docker rmi "$src_image" 2>/dev/null || true
    fi
    
    log_success "镜像同步完成！"
}

# 显示帮助信息
show_help() {
    cat << EOF
Docker Syncer 本地测试脚本

用法: bash $0 [选项]

选项:
  --single IMAGE     测试单个镜像同步（必须指定镜像名）
  --batch            测试批量同步（读取 images.txt）
  --aliyun           只同步到阿里云 ACR
  --ghcr             只同步到 GitHub Container Registry
  --double           双重同步（阿里云 + GHCR）【默认】
  --dry-run          模拟运行，不实际推送
  --help             显示此帮助信息

示例:
  # 测试单个镜像同步到阿里云
  bash $0 --single nginx:alpine --aliyun

  # 测试单个镜像双重同步
  bash $0 --single redis:7.2 --double

  # 模拟运行（不实际推送）
  bash $0 --single mysql:8.0 --dry-run

  # 批量同步（读取 images.txt）
  bash $0 --batch --double

环境变量配置:
  请在 .env.local 文件中配置所需的环境变量
  必需变量: ALIYUN_* 或 GITHUB_* (取决于同步模式)

EOF
}

# 解析命令行参数
MODE="double"
DRY_RUN="false"
TEST_MODE=""
TEST_IMAGE=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --single)
            TEST_MODE="single"
            TEST_IMAGE="$2"
            shift 2
            ;;
        --batch)
            TEST_MODE="batch"
            shift
            ;;
        --aliyun)
            MODE="aliyun"
            shift
            ;;
        --ghcr)
            MODE="ghcr"
            shift
            ;;
        --double)
            MODE="double"
            shift
            ;;
        --dry-run)
            DRY_RUN="true"
            shift
            ;;
        --help|-h)
            show_help
            exit 0
            ;;
        *)
            log_error "未知选项: $1"
            show_help
            exit 1
            ;;
    esac
done

# 验证参数
if [ -z "$TEST_MODE" ]; then
    log_error "必须指定测试模式 (--single 或 --batch)"
    show_help
    exit 1
fi

if [ "$TEST_MODE" == "single" ] && [ -z "$TEST_IMAGE" ]; then
    log_error "--single 模式必须指定镜像名"
    show_help
    exit 1
fi

# 开始测试
log_info "========================================"
log_info "Docker Syncer 本地测试"
log_info "========================================"
log_info "测试模式: $TEST_MODE"
log_info "同步模式: $MODE"
log_info "模拟运行: $DRY_RUN"
log_info "========================================"
echo

# 检查必需的环境变量
check_required_vars "$MODE"

# 登录 Docker Registry
if [ "$DRY_RUN" != "true" ]; then
    docker_login "$MODE"
    echo
fi

# 执行同步
if [ "$TEST_MODE" == "single" ]; then
    sync_single_image "$TEST_IMAGE" "$MODE" "$DRY_RUN"
elif [ "$TEST_MODE" == "batch" ]; then
    log_error "批量同步模式尚未实现，请使用 --single 模式"
    exit 1
fi

log_success "========================================"
log_success "测试完成！"
log_success "========================================"

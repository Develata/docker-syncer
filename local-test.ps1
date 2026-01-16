# ========================================
# Docker Syncer 本地测试脚本 (PowerShell)
# Local Test Script for Docker Syncer
# ========================================
#
# 使用方法：
# 1. 填写 .env.local 中的配置
# 2. 运行: .\local-test.ps1 -Mode aliyun -Image nginx:alpine
#

param(
    [Parameter(Mandatory=$false)]
    [ValidateSet("aliyun", "ghcr", "double")]
    [string]$Mode = "double",
    
    [Parameter(Mandatory=$false)]
    [string]$Image = "",
    
    [Parameter(Mandatory=$false)]
    [switch]$DryRun = $false,
    
    [Parameter(Mandatory=$false)]
    [switch]$Help = $false
)

# 颜色输出函数
function Write-ColorOutput {
    param(
        [string]$Message,
        [string]$Type = "Info"
    )
    
    switch ($Type) {
        "Info"    { Write-Host "[INFO]    " -ForegroundColor Blue -NoNewline; Write-Host $Message }
        "Success" { Write-Host "[SUCCESS] " -ForegroundColor Green -NoNewline; Write-Host $Message }
        "Warning" { Write-Host "[WARNING] " -ForegroundColor Yellow -NoNewline; Write-Host $Message }
        "Error"   { Write-Host "[ERROR]   " -ForegroundColor Red -NoNewline; Write-Host $Message }
    }
}

# 显示帮助
function Show-Help {
    @"
Docker Syncer 本地测试脚本 (PowerShell)

用法: .\local-test.ps1 [参数]

参数:
  -Mode <模式>         同步模式: aliyun, ghcr, double (默认: double)
  -Image <镜像>        要同步的镜像名 (如: nginx:alpine)
  -DryRun              模拟运行，不实际推送
  -Help                显示此帮助信息

示例:
  # 测试单个镜像同步到阿里云
  .\local-test.ps1 -Mode aliyun -Image nginx:alpine

  # 测试双重同步
  .\local-test.ps1 -Mode double -Image redis:7.2

  # 模拟运行
  .\local-test.ps1 -Image mysql:8.0 -DryRun

环境变量配置:
  请在 .env.local 文件中配置所需的环境变量
"@
}

# 加载环境变量
function Load-EnvFile {
    param([string]$FilePath = ".env.local")
    
    if (-not (Test-Path $FilePath)) {
        Write-ColorOutput ".env.local 文件不存在！请先创建并配置该文件。" "Error"
        Write-ColorOutput "提示: 已为你创建模板文件 .env.local，请填写配置信息" "Info"
        exit 1
    }
    
    Write-ColorOutput "加载环境变量: $FilePath" "Info"
    
    Get-Content $FilePath | ForEach-Object {
        $line = $_.Trim()
        # 跳过空行和注释
        if ($line -and -not $line.StartsWith("#")) {
            if ($line -match '^([^=]+)=(.*)$') {
                $key = $matches[1].Trim()
                $value = $matches[2].Trim()
                [System.Environment]::SetEnvironmentVariable($key, $value, "Process")
                # Write-ColorOutput "  $key = $value" "Info"
            }
        }
    }
    
    Write-ColorOutput "环境变量加载完成" "Success"
}

# 检查必需的环境变量
function Test-RequiredVars {
    param([string]$Mode)
    
    $missingVars = @()
    
    if ($Mode -eq "aliyun" -or $Mode -eq "double") {
        if (-not $env:ALIYUN_REGISTRY) { $missingVars += "ALIYUN_REGISTRY" }
        if (-not $env:ALIYUN_NAMESPACE) { $missingVars += "ALIYUN_NAMESPACE" }
        if (-not $env:ALIYUN_USERNAME) { $missingVars += "ALIYUN_USERNAME" }
        if (-not $env:ALIYUN_PASSWORD) { $missingVars += "ALIYUN_PASSWORD" }
    }
    
    if ($Mode -eq "ghcr" -or $Mode -eq "double") {
        if (-not $env:GITHUB_REPOSITORY_OWNER) { $missingVars += "GITHUB_REPOSITORY_OWNER" }
        if (-not $env:GITHUB_TOKEN) { $missingVars += "GITHUB_TOKEN" }
    }
    
    if ($missingVars.Count -gt 0) {
        Write-ColorOutput "缺少必需的环境变量:" "Error"
        $missingVars | ForEach-Object { Write-Host "  - $_" }
        exit 1
    }
}

# Docker 登录
function Invoke-DockerLogin {
    param([string]$Mode)
    
    try {
        if ($Mode -eq "aliyun" -or $Mode -eq "double") {
            Write-ColorOutput "登录阿里云 ACR..." "Info"
            $env:ALIYUN_PASSWORD | docker login $env:ALIYUN_REGISTRY `
                --username $env:ALIYUN_USERNAME --password-stdin
            
            if ($LASTEXITCODE -eq 0) {
                Write-ColorOutput "阿里云 ACR 登录成功" "Success"
            } else {
                Write-ColorOutput "阿里云 ACR 登录失败" "Error"
                exit 1
            }
        }
        
        if ($Mode -eq "ghcr" -or $Mode -eq "double") {
            Write-ColorOutput "登录 GitHub Container Registry..." "Info"
            $env:GITHUB_TOKEN | docker login ghcr.io `
                --username $env:GITHUB_REPOSITORY_OWNER --password-stdin
            
            if ($LASTEXITCODE -eq 0) {
                Write-ColorOutput "GHCR 登录成功" "Success"
            } else {
                Write-ColorOutput "GHCR 登录失败" "Error"
                exit 1
            }
        }
        
        # 可选：登录 Docker Hub
        if ($env:DOCKERHUB_USERNAME -and $env:DOCKERHUB_TOKEN) {
            Write-ColorOutput "登录 Docker Hub..." "Info"
            $env:DOCKERHUB_TOKEN | docker login `
                --username $env:DOCKERHUB_USERNAME --password-stdin
            
            if ($LASTEXITCODE -eq 0) {
                Write-ColorOutput "Docker Hub 登录成功" "Success"
            }
        }
    } catch {
        Write-ColorOutput "登录失败: $_" "Error"
        exit 1
    }
}

# 同步镜像
function Sync-Image {
    param(
        [string]$SourceImage,
        [string]$Mode,
        [bool]$IsDryRun
    )
    
    Write-ColorOutput "=======================================" "Info"
    Write-ColorOutput "开始同步镜像: $SourceImage" "Info"
    Write-ColorOutput "同步模式: $Mode" "Info"
    Write-ColorOutput "=======================================" "Info"
    
    # 解析镜像名和标签
    if ($SourceImage -match '^(.+?):(.+)$') {
        $imageName = $matches[1].Split('/')[-1]
        $imageTag = $matches[2]
    } else {
        $imageName = $SourceImage.Split('/')[-1]
        $imageTag = "latest"
    }
    
    Write-ColorOutput "镜像名: $imageName, 标签: $imageTag" "Info"
    
    # 拉取源镜像
    Write-ColorOutput "拉取源镜像..." "Info"
    if ($IsDryRun) {
        Write-ColorOutput "[DRY RUN] docker pull $SourceImage" "Warning"
    } else {
        docker pull $SourceImage
        if ($LASTEXITCODE -ne 0) {
            Write-ColorOutput "拉取镜像失败" "Error"
            exit 1
        }
        Write-ColorOutput "拉取完成" "Success"
    }
    
    # 同步到阿里云
    if ($Mode -eq "aliyun" -or $Mode -eq "double") {
        $aliTarget = "$env:ALIYUN_REGISTRY/$env:ALIYUN_NAMESPACE/${imageName}:${imageTag}"
        Write-ColorOutput "推送到阿里云: $aliTarget" "Info"
        
        if ($IsDryRun) {
            Write-ColorOutput "[DRY RUN] docker tag $SourceImage $aliTarget" "Warning"
            Write-ColorOutput "[DRY RUN] docker push $aliTarget" "Warning"
        } else {
            docker tag $SourceImage $aliTarget
            docker push $aliTarget
            
            if ($LASTEXITCODE -eq 0) {
                Write-ColorOutput "阿里云推送完成" "Success"
            } else {
                Write-ColorOutput "阿里云推送失败" "Error"
            }
        }
    }
    
    # 同步到 GHCR
    if ($Mode -eq "ghcr" -or $Mode -eq "double") {
        $ghcrTarget = "ghcr.io/$env:GITHUB_REPOSITORY_OWNER/${imageName}:${imageTag}".ToLower()
        Write-ColorOutput "推送到 GHCR: $ghcrTarget" "Info"
        
        if ($IsDryRun) {
            Write-ColorOutput "[DRY RUN] docker tag $SourceImage $ghcrTarget" "Warning"
            Write-ColorOutput "[DRY RUN] docker push $ghcrTarget" "Warning"
        } else {
            docker tag $SourceImage $ghcrTarget
            docker push $ghcrTarget
            
            if ($LASTEXITCODE -eq 0) {
                Write-ColorOutput "GHCR 推送完成" "Success"
            } else {
                Write-ColorOutput "GHCR 推送失败" "Error"
            }
        }
    }
    
    # 清理本地镜像
    if (-not $IsDryRun) {
        Write-ColorOutput "清理本地镜像..." "Info"
        docker rmi $SourceImage 2>$null
    }
    
    Write-ColorOutput "=======================================" "Success"
    Write-ColorOutput "镜像同步完成！" "Success"
    Write-ColorOutput "=======================================" "Success"
}

# 主程序
function Main {
    if ($Help) {
        Show-Help
        exit 0
    }
    
    if (-not $Image) {
        Write-ColorOutput "错误: 必须指定镜像名 (-Image)" "Error"
        Show-Help
        exit 1
    }
    
    Write-ColorOutput "=======================================" "Info"
    Write-ColorOutput "Docker Syncer 本地测试 (PowerShell)" "Info"
    Write-ColorOutput "=======================================" "Info"
    Write-ColorOutput "测试镜像: $Image" "Info"
    Write-ColorOutput "同步模式: $Mode" "Info"
    Write-ColorOutput "模拟运行: $DryRun" "Info"
    Write-ColorOutput "=======================================" "Info"
    Write-Host ""
    
    # 加载环境变量
    Load-EnvFile
    Write-Host ""
    
    # 检查必需的环境变量
    Test-RequiredVars -Mode $Mode
    Write-Host ""
    
    # 登录
    if (-not $DryRun) {
        Invoke-DockerLogin -Mode $Mode
        Write-Host ""
    }
    
    # 执行同步
    Sync-Image -SourceImage $Image -Mode $Mode -IsDryRun $DryRun
}

# 执行主程序
Main

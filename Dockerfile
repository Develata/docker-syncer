FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

# 1. 安装基础工具
RUN apt-get update && apt-get install -y \
    ca-certificates \
    curl \
    gnupg \
    skopeo \
    && rm -rf /var/lib/apt/lists/*

# 2. 添加 Docker 官方 GPG Key 和 软件源
# 这一步是为了确保我们能下载到最新的 Docker 客户端 (支持 API 1.44+)
RUN install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg && \
    chmod a+r /etc/apt/keyrings/docker.gpg && \
    echo \
    "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian \
    $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
    tee /etc/apt/sources.list.d/docker.list > /dev/null

# 3. 安装最新版 Docker CLI (docker-ce-cli)
RUN apt-get update && \
    apt-get install -y docker-ce-cli && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app
CMD ["/bin/bash"]
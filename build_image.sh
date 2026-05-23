#!/usr/bin/env bash
# 本地构建 amd64 镜像的快速脚本。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

docker build --platform linux/amd64 \
  -t xeasydata.cn:5005/sub2api:latest \
  --build-arg GOPROXY=https://goproxy.cn,direct \
  --build-arg GOSUMDB=sum.golang.google.cn \
  -f "${SCRIPT_DIR}/Dockerfile" \
  "${SCRIPT_DIR}"

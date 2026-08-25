# BENZHI_README

基于 Go 实现的濒危植物种子萌发试验封存台 Web 项目，一款后端服务，濒危植物种子萌发试验封存台提供从种源建档、试验设计、分阶段观测、异常裁决、同行复核到冻结封存和凭据验证的可追溯治理流程。

## 项目说明
- 项目：benzhi-project-dd37b4ae-2e93-4a74-9fbe-939d2c72a50e
- 项目用途：濒危植物种子萌发试验封存台提供从种源建档、试验设计、分阶段观测、异常裁决、同行复核到冻结封存和凭据验证的可追溯治理流程。
- Go 工具链：`golang:1.23`
- 前端工具链：原生 HTML、CSS 和 JavaScript，由 Go 服务直接提供

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/seedvault -selfcheck -addr=127.0.0.1:19081
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-dd37b4ae-2e93-4a74-9fbe-939d2c72a50e-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-dd37b4ae-2e93-4a74-9fbe-939d2c72a50e-arm64 linux/arm64
docker run -it benzhi-project-dd37b4ae-2e93-4a74-9fbe-939d2c72a50e-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/seedvault -selfcheck -addr=127.0.0.1:19081`

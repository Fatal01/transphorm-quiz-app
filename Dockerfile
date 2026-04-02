# ── 构建阶段 ──────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

# MySQL 驱动为纯 Go 实现，无需 CGO，不需要 gcc/sqlite

WORKDIR /app

# 先拉依赖（利用 Docker 层缓存）
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# 复制源码并编译（静态二进制，无外部依赖）
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o quiz-server .

# ── 运行阶段 ──────────────────────────────────────────────
FROM alpine:latest

# ca-certificates：HTTPS 请求；tzdata：时区支持
RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /app/quiz-server .

# 复制前端静态文件
COPY backend/public ./public

# 静态资源目录（背景图等运行时上传文件）
RUN mkdir -p /app/static

EXPOSE 8080

CMD ["./quiz-server"]

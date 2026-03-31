# 构建阶段
FROM golang:1.25-alpine AS builder

# 安装CGO依赖（sqlite3需要）
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app

# 复制go.mod和go.sum
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# 复制源码
COPY backend/ ./

# 编译
RUN CGO_ENABLED=1 GOOS=linux go build -o quiz-server .

# 运行阶段
FROM alpine:latest

# 安装sqlite运行时依赖
RUN apk add --no-cache sqlite-libs ca-certificates tzdata

# 设置时区为上海
ENV TZ=Asia/Shanghai

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /app/quiz-server .

# 复制前端静态文件
COPY backend/public ./public

# 创建静态资源目录和数据目录（static 由运行时挂载 /data 提供）
RUN mkdir -p /data /app/static

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["./quiz-server"]

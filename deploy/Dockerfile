# ---------- 构建阶段 ----------
FROM golang:1.27-alpine AS builder

# 国内环境默认走代理下载依赖；可通过 --build-arg GOPROXY=... 覆盖。
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /src

# 依赖层单独缓存：只要 go.mod / go.sum 不变就不重复下载。
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# 纯 Go 静态编译：modernc.org/sqlite 不依赖 CGO，运行镜像无需 glibc。
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/ulpd ./cmd/ulpd \
 && CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/ulp-verify ./cmd/verify

# ---------- 运行阶段 ----------
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -g 10001 -S ulp \
 && adduser -u 10001 -S -G ulp -h /app ulp \
 && mkdir -p /data/licenses \
 && chown -R ulp:ulp /data /app

WORKDIR /app

COPY --from=builder /out/ulpd /usr/local/bin/ulpd
COPY --from=builder /out/ulp-verify /usr/local/bin/ulp-verify

# SQLite 数据库、签发的 .lic 文件与回拨检查点都落在同一个数据卷。
VOLUME ["/data"]

ENV ULP_ADDR=":7630" \
    ULP_DATA_DIR="/data" \
    TZ="Asia/Shanghai"

USER ulp

EXPOSE 7630

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:7630/api/health || exit 1

ENTRYPOINT ["/usr/local/bin/ulpd"]

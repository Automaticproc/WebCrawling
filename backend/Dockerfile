FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/catalogd ./cmd/server

FROM debian:bookworm-slim
ARG INSTALL_BROWSER=0
RUN set -eux; \
    sed -i 's|deb.debian.org|mirrors.aliyun.com|g' /etc/apt/sources.list.d/debian.sources; \
    apt-get -o Acquire::Retries=8 update; \
    apt-get -o Acquire::Retries=8 install -y --no-install-recommends ca-certificates tzdata ffmpeg curl; \
    if [ "$INSTALL_BROWSER" = "1" ]; then \
      apt-get -o Acquire::Retries=8 install -y --no-install-recommends chromium fonts-liberation; \
      ln -sf /usr/bin/chromium /usr/bin/chromium-browser; \
      /usr/bin/chromium --version; \
    fi; \
    rm -rf /var/lib/apt/lists/*
ENV TZ=Asia/Hong_Kong
# 容器里以 root 运行，Chromium 必须关掉自带沙箱
ENV DOWNLOAD_NO_SANDBOX=1
COPY --from=build /out/catalogd /usr/local/bin/catalogd
EXPOSE 43123
ENTRYPOINT ["catalogd"]

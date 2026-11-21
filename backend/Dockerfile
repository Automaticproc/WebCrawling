FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/catalogd ./cmd/server

FROM debian:bookworm-slim
ARG INSTALL_BROWSER=0
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata ffmpeg \
    && if [ "$INSTALL_BROWSER" = "1" ]; then \
         apt-get install -y --no-install-recommends chromium fonts-liberation; \
         ln -sf /usr/bin/chromium /usr/bin/chromium-browser; \
       fi \
    && rm -rf /var/lib/apt/lists/*
ENV TZ=Asia/Hong_Kong
ENV DOWNLOAD_NO_SANDBOX=1
COPY --from=build /out/catalogd /usr/local/bin/catalogd
EXPOSE 43123
ENTRYPOINT ["catalogd"]

# 基於官方 playwright-go 範例的 Dockerfile
# Stage 1: Modules caching
FROM golang:1.24.2 as modules
COPY go.mod go.sum /modules/
WORKDIR /modules
RUN go mod download

# Stage 2: Build
FROM golang:1.24.2 as builder
COPY --from=modules /go/pkg /go/pkg
COPY . /workdir
WORKDIR /workdir
# Install playwright CLI with right version
RUN PWGO_VER=$(grep -oE "playwright-go v\S+" /workdir/go.mod | sed 's/playwright-go //g') \
    && go install github.com/playwright-community/playwright-go/cmd/playwright@${PWGO_VER}
RUN GOOS=linux GOARCH=amd64 go build -o /bin/app

# Stage 3: Final
FROM ubuntu:22.04
COPY --from=builder /go/bin/playwright /bin/app /
COPY config.yml /config.yml
RUN apt-get update && apt-get install -y ca-certificates tzdata curl \
    # Install dependencies and all browsers
    && /playwright install --with-deps \
    && rm -rf /var/lib/apt/lists/*
EXPOSE 8090
CMD ["/app"]
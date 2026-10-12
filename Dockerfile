# syntax=docker/dockerfile:1
ARG GO_VERSION=1.27.2
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG GOPROXY=https://proxy.golang.org,direct
ARG VERSION=0.5.0-alpha.1
ARG COMMIT=unknown
ENV GOTOOLCHAIN=local
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X fakeapi/internal/cli.Version=${VERSION} -X fakeapi/internal/cli.Commit=${COMMIT}" -o /out/fakeapi ./cmd/fakeapi
RUN mkdir -p /out/data && chown 65532:65532 /out/data
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/fakeapi /fakeapi
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
WORKDIR /data
EXPOSE 8787
ENTRYPOINT ["/fakeapi"]

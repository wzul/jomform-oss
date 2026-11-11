# Pinned to the patch release that clears BOTH the GO-2026-6617 class of stdlib
# advisories (net/http HPACK, html/template, crypto/tls, mime/multipart, os —
# all "Fixed in go1.27.2") AND the crypto/x509 change in go1.27.3 that refuses
# certificate chains through the eight eMudhra intermediate CA keys revoked for
# cACompromise (CVE-2026-111943 / go.dev/issue/82345). 1.27.4 adds the fix for
# GO-2026-6859 / CVE-2026-97054 (published 2026-11-11): HTTP/1 MaxBytesReader
# sets `Connection: close` unsynchronised, so a handler that reads the body
# while writing a response header can panic — "Fixed in net/http@go1.27.4",
# affecting go1.27.0-0 through go1.27.3 across 87 symbols of the net/http server
# surface, which is the whole of this api. This line is the toolchain
# the SHIPPED binary is built with, so it decides which stdlib — and which
# intermediates are trusted — prod runs with, on every outbound TLS call: CHIP,
# SES, S3/MinIO, Cloudflare, MCP OAuth providers and merchant-configured webhook
# URLs (any host in the world).
# api/go.mod carries `toolchain go1.27.4` for the host gates, but this image runs
# GOTOOLCHAIN=local (its own /usr/local/go/go.env), so a `toolchain` line is
# IGNORED here and only the base image decides which stdlib the shipped binary is
# built against.
# api/internal/version/go_toolchain_pin_test.go fails the build if this tag — or
# go.mod's `toolchain` line — drops below go1.27.4. It exists because govulncheck
# reported "No vulnerabilities found" while 1.27.2 was pinned (the advisory DB
# had not caught up), so the govuln gate would NOT have forced this fix.
FROM golang:1.27.4-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Provenance stamps. `deploy/build-and-ship.sh` passes all three as --build-arg
# (VERSION from `git describe`, COMMIT from `git rev-parse --short`, BUILD_DATE
# from `date -u`), and each one is wired into the binary below so /version,
# `--version` and the startup banner report the same values the OCI labels
# carry. The defaults here mirror api/internal/version/version.go exactly, so a
# bare `docker build` still reports dev/none/unknown instead of an empty string.
# api/internal/version/dockerfile_wiring_test.go fails the build if a version
# variable is added without both an ARG and its -X wiring.
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X github.com/wzul/jomform/api/internal/version.Version=${VERSION} -X github.com/wzul/jomform/api/internal/version.Commit=${COMMIT} -X github.com/wzul/jomform/api/internal/version.Date=${BUILD_DATE}" \
  -o /jomform-api ./cmd/jomform-api

FROM alpine:3.20
# ghostscript is required for auto-generated PDF preview thumbnails (issue
# #383): the api renders the first page of an uploaded PDF to a JPEG with the
# `gs` binary (WordPress-style). Without it, PDF uploads still work but get no
# preview image.
RUN apk add --no-cache ca-certificates tzdata ghostscript
COPY --from=build /jomform-api /usr/local/bin/jomform-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/jomform-api"]
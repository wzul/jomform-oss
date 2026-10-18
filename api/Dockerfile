# Pinned to the patch release that clears the GO-2026-6617 class of stdlib
# advisories (net/http HPACK, html/template, crypto/tls, mime/multipart,
# os — all "Fixed in go1.27.2"). api/go.mod carries `toolchain go1.27.2` for
# the host gates, but this image runs GOTOOLCHAIN=local (its own
# /usr/local/go/go.env), so a `toolchain` line is IGNORED here and only the
# base image decides which stdlib the shipped binary is built against.
FROM golang:1.27.2-alpine AS build
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
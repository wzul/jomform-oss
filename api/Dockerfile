FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X github.com/wzul/jomform/api/internal/version.Version=${VERSION} -X github.com/wzul/jomform/api/internal/version.Commit=${COMMIT}" \
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
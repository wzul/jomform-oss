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
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /jomform-api /usr/local/bin/jomform-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/jomform-api"]
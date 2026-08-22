FROM golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG REVISION=unknown

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build \
    -mod=readonly \
    -trimpath \
    -buildvcs=false \
    -ldflags="-s -w -buildid= -X main.version=${VERSION} -X main.revision=${REVISION}" \
    -o /out/dar-download \
    ./cmd/dar-download

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7

LABEL org.opencontainers.image.title="DAR Download Application" \
      org.opencontainers.image.description="Private authenticated DAR streaming service" \
      org.opencontainers.image.source="https://github.com/SalehElnagar/dar-download-app" \
      org.opencontainers.image.licenses="LicenseRef-Proprietary"

COPY --from=build --chown=65532:65532 /out/dar-download /dar-download

USER 65532:65532
EXPOSE 8000

ENTRYPOINT ["/dar-download"]

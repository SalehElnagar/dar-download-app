FROM golang:1.27.1-alpine3.23@sha256:d9e2f2f07b10cc922da3e80e035c3058810b328d5aef82d2c63680967c5e2ec9 AS build

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

FROM gcr.io/distroless/static-debian13:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6

LABEL org.opencontainers.image.title="DAR Download Application" \
      org.opencontainers.image.description="Private authenticated DAR streaming service" \
      org.opencontainers.image.source="https://github.com/SalehElnagar/dar-download-app" \
      org.opencontainers.image.licenses="LicenseRef-Proprietary"

COPY --from=build --chown=65532:65532 /out/dar-download /dar-download

USER 65532:65532
EXPOSE 8000

ENTRYPOINT ["/dar-download"]

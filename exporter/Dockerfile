# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/mihomo-k8s-exporter .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/mihomo-k8s-exporter /usr/local/bin/mihomo-k8s-exporter
USER 65532:65532
EXPOSE 9101
ENTRYPOINT ["/usr/local/bin/mihomo-k8s-exporter"]

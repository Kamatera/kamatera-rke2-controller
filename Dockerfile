FROM golang:1.25 AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags="-s -w" -o /out/kamatera-rke2-controller ./cmd/controller

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/kamatera-rke2-controller /kamatera-rke2-controller

USER 65532:65532
ENTRYPOINT ["/kamatera-rke2-controller"]

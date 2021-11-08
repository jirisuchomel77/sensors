FROM golang:1.16 as builder

ARG LDFLAGS="-w -s"

WORKDIR /workspace

COPY go.mod go.sum ./
COPY sensors.go ./

RUN go mod download

RUN CGO_ENABLED=0 GO111MODULE=on go build -a -ldflags "$LDFLAGS" -o sensors

# Move the binary to smaller image
FROM alpine
WORKDIR /

RUN adduser -D app
USER app

COPY --from=builder --chown=app /workspace/sensors .

ENTRYPOINT ["/sensors"]

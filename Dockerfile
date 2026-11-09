# Dockerfile for the Go components (all five services + operator).
# One image; the subcommand selects the component.

FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache dependencies first for faster rebuilds
COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
COPY config/ config/
COPY helpers/ helpers/
COPY protos/ protos/
COPY data_acquisition/ data_acquisition/
COPY llm_interface/ llm_interface/
COPY safety_policy/ safety_policy/
COPY scaling_executor/ scaling_executor/
COPY operator/ operator/

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/llm-autoscaler .

FROM alpine:latest

RUN addgroup -g 1000 -S appgroup && adduser -u 1000 -S appuser -G appgroup

COPY --from=builder /out/llm-autoscaler /usr/local/bin/llm-autoscaler
COPY NOTICE /app/NOTICE
COPY LICENSE /app/LICENSE

USER appuser

ENTRYPOINT ["/usr/local/bin/llm-autoscaler"]

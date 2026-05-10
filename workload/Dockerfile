FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY workload /src/workload

RUN CGO_ENABLED=0 GOOS=linux go build -o /out/workload ./workload

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/workload /usr/local/bin/workload

ENTRYPOINT ["/usr/local/bin/workload"]

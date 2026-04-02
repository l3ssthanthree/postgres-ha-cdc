FROM golang:1.26-alpine

WORKDIR /app

RUN apk add --no-cache bash postgresql-client

RUN go install github.com/pressly/goose/v3/cmd/goose@latest
RUN go install github.com/realkarych/seqwall@latest

COPY go.mod /app/
COPY go.sum /app/
RUN go mod download

COPY cmd /app/cmd
COPY scripts /app/scripts
COPY migrations /app/migrations

RUN chmod +x /app/scripts/*.sh

ENTRYPOINT ["/app/scripts/entrypoint.sh"]
FROM golang:1.26-alpine AS exporter-builder

WORKDIR /src

COPY backup_exporter ./backup_exporter
RUN GO111MODULE=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/backup-exporter ./backup_exporter

FROM minio/mc:latest AS mc

FROM postgres:18-alpine

RUN apk add --no-cache busybox-suid ca-certificates tzdata

COPY --from=mc /usr/bin/mc /usr/local/bin/mc
COPY --from=exporter-builder /out/backup-exporter /usr/local/bin/backup-exporter
COPY scripts/backup /opt/backup
COPY scripts/minio /opt/minio

RUN chmod +x /usr/local/bin/mc /usr/local/bin/backup-exporter /opt/backup/*.sh /opt/minio/*.sh

EXPOSE 9091

ENTRYPOINT ["/opt/backup/entrypoint.sh"]

FROM postgres:18

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        gcc \
        libpq-dev \
        python3 \
        python3-dev \
        python3-pip \
        python3-venv \
    && rm -rf /var/lib/apt/lists/*

RUN pip3 install --break-system-packages --no-cache-dir "patroni[etcd3]" psycopg2-binary

COPY patroni/entrypoint.sh /usr/local/bin/patroni-entrypoint.sh
COPY scripts/patroni/post-init.sh /usr/local/bin/patroni-post-init.sh

RUN chmod +x /usr/local/bin/patroni-entrypoint.sh /usr/local/bin/patroni-post-init.sh

ENTRYPOINT ["/usr/local/bin/patroni-entrypoint.sh"]

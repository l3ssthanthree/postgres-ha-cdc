> Это руководство перенесено из учебного проекта. Пароли в командах — публичные демонстрационные значения. Команда `docker compose down -v` удаляет данные томов; для обычного запуска используй команды из корневого README.

# Этап 7.2. BI-метрики через PostgreSQL -> Debezium -> Kafka -> ClickHouse -> Metabase

## Выбранная таблица

Для CDC и BI выбрана таблица `public.order_event_log`.

Почему она подходит под ТЗ:

- это журнал событий заказов, то есть быстрорастущая append-heavy таблица;
- она заполняется сидами: для каждого заказа создаётся 12 событий;
- есть время события: `created_at`;
- есть категориальные срезы: `event_type`, `actor_type`;
- есть числовые показатели внутри `payload`: `step`, `status_id`;
- по ней можно строить не только `count(*)`, но и события по дням, типам, актёрам, затронутым заказам и среднему шагу процесса.

## Что добавлено

- PostgreSQL/Patroni:
  - `wal_level=logical`;
  - роль `fruits_debezium` с `REPLICATION LOGIN`;
  - база и роль `metabase` для метаданных Metabase;
  - миграция `012_order_event_log_cdc.sql` с publication и правами.
- Kafka:
  - один брокер `kafka` в KRaft-режиме без ZooKeeper;
  - образ Kafka зафиксирован как `apache/kafka:3.9.1`, потому что `latest` может уехать на Kafka 4.x и ломать совместимость с ClickHouse Kafka Engine;
  - `kafka-ui` на `http://127.0.0.1:8084`.
- Kafka Connect + Debezium:
  - `bi/debezium/postgres-connector.json`;
  - идемпотентная регистрация через `bi/debezium/register.ps1` или `bi/debezium/register.sh`.
- ClickHouse:
  - Kafka Engine table `fruits_bi.order_event_log_queue`;
  - MergeTree table `fruits_bi.order_event_log_cdc`;
  - materialized view `fruits_bi.mv_order_event_log_cdc`;
  - BI views для Metabase.
- Metabase:
  - сервис на `http://127.0.0.1:3001`;
  - metadata хранится в PostgreSQL;
  - аналитика подключается к ClickHouse.

## Как запустить

Если стенд уже запускался до добавления logical replication, проще всего поднять его начисто:

```powershell
docker compose down -v
docker compose up -d --build
```

Если удалять volumes нельзя, подними обычным способом, но проверь, что у PostgreSQL стал `wal_level=logical`:

```powershell
docker compose up -d --build
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -U fruits_admin -d fruits_db -c "SHOW wal_level;"
```

Дождись, пока основные сервисы станут healthy:

```powershell
docker compose ps
```

Зарегистрируй Debezium-коннектор:

```powershell
.\bi\debezium\register.ps1
```

Или через Git Bash/WSL:

```bash
sh bi/debezium/register.sh
```

Проверь статус:

```powershell
curl.exe http://localhost:8083/connectors/postgres-connector/status
curl.exe http://localhost:8083/connectors
```

## Ручная end-to-end проверка

Сгенерировать INSERT, UPDATE и DELETE в `order_event_log`:

```powershell
Get-Content .\bi\sql\order_event_log_cdc_smoke.sql | docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -U fruits_admin -d fruits_db
```

Посмотреть CDC-сообщения в Kafka:

```powershell
docker compose exec -T kafka /opt/kafka/bin/kafka-console-consumer.sh --bootstrap-server kafka:9092 --topic fruits.public.order_event_log --from-beginning --max-messages 3
```

Проверить строки в ClickHouse:

```powershell
docker compose exec -T clickhouse clickhouse-client --query "SELECT count(), max(kafka_loaded_at) FROM fruits_bi.order_event_log_cdc"
docker compose exec -T clickhouse clickhouse-client --query "SELECT * FROM fruits_bi.v_bi_order_event_kpi FORMAT Vertical"
docker compose exec -T clickhouse clickhouse-client --query "SELECT * FROM fruits_bi.v_bi_order_events_by_type LIMIT 10"
```

Kafka UI:

```text
http://127.0.0.1:8084
```

Нужный топик:

```text
fruits.public.order_event_log
```

## Metabase

Открой:

```text
http://127.0.0.1:3001
```

При первом запуске создай администратора через мастер настройки Metabase.
Учётная запись и BI-дашборд из учебного стенда не входят в репозиторий.

Metadata Metabase настроена через переменные `MB_DB_*` и хранится в PostgreSQL-базе `metabase`.

Если стенд поднимался до добавления Metabase и volumes нельзя удалить, создай metadata-роль и БД вручную:

```powershell
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d postgres -c "CREATE ROLE metabase LOGIN PASSWORD 'metabase_password';"
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d postgres -c "CREATE DATABASE metabase OWNER metabase;"
docker compose restart metabase
```

Добавь аналитическую базу ClickHouse:

- Database type: `ClickHouse`;
- Host: `clickhouse`;
- Port: `8123`;
- Database name: `fruits_bi`;
- Username: `metabase_ch`;
- Password: `metabase_ch_password`.

В проверенном стенде эта база добавлена как `Fruits BI ClickHouse`, а дашборд создан как `Order Event Log BI` с тремя карточками: KPI, line chart и bar chart.

Если в списке Metabase нет ClickHouse, положи jar-файл ClickHouse-драйвера Metabase в `metabase/plugins` и перезапусти сервис:

```powershell
docker compose restart metabase
```

## Минимум 3 визуализации

KPI / Number:

```sql
SELECT
    current_events,
    affected_orders,
    avg_events_per_order,
    avg_workflow_step,
    newest_event_at
FROM fruits_bi.v_bi_order_event_kpi;
```

Line chart:

```sql
SELECT
    day,
    events_count,
    affected_orders
FROM fruits_bi.v_bi_order_events_by_day
ORDER BY day;
```

Bar chart:

```sql
SELECT
    event_type,
    events_count,
    affected_orders,
    avg_workflow_step
FROM fruits_bi.v_bi_order_events_by_type
ORDER BY events_count DESC;
```

Pie chart или дополнительный bar chart:

```sql
SELECT
    actor_type,
    events_count
FROM fruits_bi.v_bi_order_events_by_actor
ORDER BY events_count DESC;
```

## Что делает добавленный код

### `patroni/entrypoint.sh`

- `DEBEZIUM_DB_USER`, `DEBEZIUM_DB_PASSWORD` задают пользователя, которым Debezium подключается к PostgreSQL.
- `METABASE_DB_NAME`, `METABASE_DB_USER`, `METABASE_DB_PASSWORD` задают отдельную БД и роль для metadata Metabase.
- Добавление этих переменных в `post-init.env` передаёт их в `scripts/patroni/post-init.sh`.
- `wal_level: logical` включает logical decoding, без него Debezium не может читать изменения через replication slot.
- `host replication ${DEBEZIUM_DB_USER} ...` разрешает replication-подключение для Debezium.
- `host all ${DEBEZIUM_DB_USER} ...` разрешает обычное SQL-подключение, которое Debezium использует для snapshot и чтения схемы.

### `scripts/patroni/post-init.sh`

- `CREATE ROLE ... REPLICATION LOGIN` создаёт роль Debezium.
- `ALTER ROLE ... REPLICATION LOGIN` делает скрипт идемпотентным: повторный запуск обновит пароль и права.
- `CREATE ROLE metabase ...` создаёт пользователя metadata-хранилища Metabase.
- `CREATE DATABASE metabase OWNER metabase` создаёт отдельную БД, где Metabase хранит пользователей, вопросы и дашборды.
- `GRANT CONNECT ON DATABASE ... TO ...` явно разрешает нужным сервисным пользователям подключаться к нужным БД.

### `migrations/012_order_event_log_cdc.sql`

- `CREATE ROLE fruits_debezium ...` страхует существующие стенды, где `post-init` уже не выполнится повторно.
- `GRANT USAGE ON SCHEMA public` разрешает Debezium видеть схему `public`.
- `GRANT SELECT ON TABLE order_event_log` нужен для initial snapshot.
- `ALTER TABLE order_event_log REPLICA IDENTITY FULL` заставляет PostgreSQL писать в WAL полный образ строки для UPDATE/DELETE.
- `CREATE PUBLICATION ... FOR TABLE order_event_log` ограничивает CDC только выбранной таблицей.

### `bi/debezium/postgres-connector.json`

- `connector.class` выбирает PostgreSQL Debezium connector.
- `plugin.name=pgoutput` включает штатный PostgreSQL logical decoding plugin.
- `database.hostname=db` ведёт Debezium через HAProxy на текущий primary.
- `topic.prefix=fruits` даёт Kafka topic `fruits.public.order_event_log`.
- `table.include.list=public.order_event_log` ограничивает поток одной таблицей.
- `slot.name` задаёт persistent logical replication slot.
- `publication.name` говорит использовать publication из миграции.
- `snapshot.mode=when_needed` при первом запуске выгружает уже существующие строки, затем переходит в streaming; после failover или пересоздания primary он может заново снять snapshot, если старый WAL LSN больше недоступен.
- `tombstones.on.delete=false` отключает tombstone-сообщения, чтобы ClickHouse читал только Debezium change event.

### `clickhouse/init/001_order_event_log_bi.sql`

- `CREATE DATABASE fruits_bi` создаёт аналитическую БД.
- `order_event_log_queue ENGINE = Kafka` читает JSON-сообщения из Kafka topic.
- `order_event_log_cdc ENGINE = ReplacingMergeTree` хранит версии строк из CDC.
- `mv_order_event_log_cdc` разбирает Debezium envelope: берёт `after` для INSERT/UPDATE/snapshot и `before` для DELETE.
- `JSONExtractString(row_json, 'payload')` нужен потому, что Debezium отдаёт PostgreSQL `jsonb` как строку с JSON внутри; после этого из неё достаются `step` и `status_id`.
- `op` хранит тип операции: `c`, `r`, `u`, `d`.
- `is_deleted` помечает удалённые строки.
- `event_version` берётся из WAL LSN, чтобы более поздняя версия строки побеждала раннюю.
- `v_order_event_log_current` собирает текущее состояние строк через `argMax(..., event_version)` и исключает deletes.
- `v_bi_order_event_kpi` даёт KPI.
- `v_bi_order_events_by_day` даёт данные для line chart.
- `v_bi_order_events_by_type` даёт данные для bar chart по типам событий.
- `v_bi_order_events_by_actor` даёт срез по источнику события.
- `CREATE USER metabase_ch` и `GRANT SELECT` дают Metabase read-only доступ к аналитике.

### `clickhouse/config.d/limits.xml`

- `max_memory_usage`, `max_threads`, `max_concurrent_queries` ограничивают ресурсы ClickHouse для локального стенда.
- `<kafka><consumer><auto_offset_reset>smallest</auto_offset_reset></consumer></kafka>` заставляет новый ClickHouse consumer читать CDC topic с начала, чтобы initial snapshot Debezium не пропал.

### `docker-compose.yml`

- `kafka` запускается в KRaft-режиме без ZooKeeper.
- `KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://kafka:9092` даёт единый адрес брокера для Kafka Connect, ClickHouse и kafka-ui внутри compose-сети.
- `kafka-connect` публикует REST API на `8083` и хранит свои служебные offsets/config/status topics в Kafka.
- `clickhouse` монтирует XML-конфиг и SQL-init для аналитических таблиц.
- `metabase` публикуется на `3001`, потому что `3000` уже занят Grafana.

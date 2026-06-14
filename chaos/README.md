# Этап 7.1. Chaos-проверка отказоустойчивости PostgreSQL

Цель: руками проверить, что кластер Patroni + etcd + HAProxy переживает отказ primary PostgreSQL и корректно ведет себя при потере DCS.

## Основная информация

Состав стенда:

- `patroni1`, `patroni2`, `patroni3` - PostgreSQL-узлы под Patroni.
- `etcd1`, `etcd2`, `etcd3` - DCS-кластер для выборов лидера.
- `db` - HAProxy, единая точка входа в текущий primary PostgreSQL.
- `prometheus`, `grafana` - мониторинг.

Внешние адреса:

- Grafana: `http://127.0.0.1:3000`
- Prometheus: `http://127.0.0.1:9090`
- HAProxy stats: `http://127.0.0.1:7000/`
- HAProxy metrics: `http://127.0.0.1:7000/metrics`
- Patroni API: `http://127.0.0.1:8008`, `8009`, `8010`
- Workload health: `http://127.0.0.1:8080/healthz`

Grafana dashboard для экспериментов: `Chaos HA Overview`.

Главная формула на память:

```powershell
docker compose up --build -d
docker compose exec -T patroni1 patronictl -c /etc/patroni/patroni.yml list
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d fruits_db -c "SELECT inet_server_addr(), pg_is_in_recovery();"
```

Если `pg_is_in_recovery()` равно `f`, HAProxy ведет на primary.

## Проверка перед экспериментами

Поднять проект:

```powershell
docker compose up --build -d
```

Посмотреть контейнеры:

```powershell
docker compose ps
```

Посмотреть роли Patroni:

```powershell
docker compose exec -T patroni1 patronictl -c /etc/patroni/patroni.yml list
```

Проверить etcd:

```powershell
docker compose exec -T etcd1 etcdctl --endpoints=http://etcd1:2379,http://etcd2:2379,http://etcd3:2379 endpoint health
```

Проверить доступ к БД через HAProxy:

```powershell
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d fruits_db -c "SELECT inet_server_addr(), inet_server_port(), pg_is_in_recovery();"
```

Проверить, что запись проходит через HAProxy:

```powershell
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d fruits_db -c "CREATE TABLE IF NOT EXISTS chaos_ha_check(id bigserial primary key, created_at timestamptz default now()); INSERT INTO chaos_ha_check DEFAULT VALUES RETURNING id, inet_server_addr(), pg_is_in_recovery(); SELECT count(*) FROM chaos_ha_check;"
```

Открыть мониторинг:

```powershell
start http://127.0.0.1:3000
start http://127.0.0.1:7000/
```

## Инструмент инъекции отказов

Используется Pumba:

```powershell
docker run --rm gaiaadm/pumba:latest --version
```

Pumba запускается как отдельный контейнер и управляет другими контейнерами через Docker socket:

```powershell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gaiaadm/pumba:latest --help
```

Для временной остановки контейнера используется команда:

```powershell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gaiaadm/pumba:latest stop --restart --duration 45s fruits_patroni1
```

Важное правило: `--duration` для `stop` работает вместе с `--restart`.

Для потери связи с etcd удобнее использовать `pause`, потому что в compose у etcd есть автоперезапуск и простая остановка может быстро поднять контейнер обратно:

```powershell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gaiaadm/pumba:latest pause --duration 45s fruits_etcd1 fruits_etcd2 fruits_etcd3
```

## Сценарий 1 - отказ primary PostgreSQL

Что проверяем:

- Patroni автоматически выбирает новый primary.
- HAProxy начинает вести клиентов на новый primary.
- Старый primary после возврата входит в кластер как replica.
- У клиентов возможна короткая пауза или ошибка на время выборов и healthcheck.

Шаг 1. Найти текущий primary:

```powershell
docker compose exec -T patroni1 patronictl -c /etc/patroni/patroni.yml list
```

В таблице строка с ролью `Leader` - это текущий primary.

Соответствие имен:

- `patroni1` -> `fruits_patroni1`
- `patroni2` -> `fruits_patroni2`
- `patroni3` -> `fruits_patroni3`

Шаг 2. Уронить текущий primary через Pumba.

Если primary на `patroni1`:

```powershell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gaiaadm/pumba:latest stop --restart --duration 45s fruits_patroni1
```

Если primary на `patroni2`:

```powershell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gaiaadm/pumba:latest stop --restart --duration 45s fruits_patroni2
```

Если primary на `patroni3`:

```powershell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gaiaadm/pumba:latest stop --restart --duration 45s fruits_patroni3
```

Шаг 3. Проверить переключение:

```powershell
docker compose exec -T patroni2 patronictl -c /etc/patroni/patroni.yml list
```

Если остановлен `patroni2`, выполняй эту же команду через `patroni1` или `patroni3`.

Шаг 4. Проверить HAProxy:

```powershell
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d fruits_db -c "INSERT INTO chaos_ha_check DEFAULT VALUES RETURNING id, inet_server_addr(), pg_is_in_recovery(); SELECT count(*) FROM chaos_ha_check;"
```

Ожидаемый результат:

- В `patronictl list` новый узел стал `Leader`.
- SQL-запрос через `db:5432` снова выполняется.
- `pg_is_in_recovery()` равно `f`, то есть HAProxy ведет на primary.
- В Grafana на панели `Patroni Primary Role` лидер меняется с одного узла на другой.
- В HAProxy stats активным становится backend нового primary.

Шаг 5. Проверить восстановление старого узла:

```powershell
docker compose ps
docker compose exec -T patroni1 patronictl -c /etc/patroni/patroni.yml list
```

Старый узел после возврата не обязан снова стать primary. Нормально, если он вернулся как `Replica`.

Краткий вывод для отчета:

> При остановке текущего primary Patroni автоматически выбрал новый primary из оставшихся replica. HAProxy перестал отправлять соединения на остановленный узел и после healthcheck начал отправлять их на новый primary. Во время выборов возможна кратковременная ошибка подключения, но после завершения failover запись через HAProxy восстановилась. Возвращенный узел подключился обратно как replica.

Фактический прогон в этом проекте:

- До отказа `patroni1` был `Leader`.
- После `pumba stop --restart --duration 45s fruits_patroni1` новым `Leader` стал `patroni3`.
- `patroni1` вернулся как `Replica`, обе replica были в состоянии `streaming`, лаг `0`.
- SQL-запись через HAProxy после failover прошла успешно, `pg_is_in_recovery()` было `f`.

## Сценарий 2 - потеря связи с etcd

Что проверяем:

- etcd становится недоступным для Patroni.
- Patroni теряет DCS и не может безопасно выполнять failover.
- Текущий primary при потере возможности продлить лидерский lock может демотироваться.
- После возврата etcd управляемость кластера восстанавливается.

Шаг 1. Проверить etcd до отказа:

```powershell
docker compose exec -T etcd1 etcdctl --endpoints=http://etcd1:2379,http://etcd2:2379,http://etcd3:2379 endpoint health
docker compose exec -T patroni1 patronictl -c /etc/patroni/patroni.yml list
```

Шаг 2. Заморозить все etcd-узлы через Pumba на 45-60 секунд:

```powershell
docker run --rm -v /var/run/docker.sock:/var/run/docker.sock gaiaadm/pumba:latest pause --duration 45s fruits_etcd1 fruits_etcd2 fruits_etcd3
```

Пояснение: если остановить только один `etcd`, quorum 2/3 сохранится и DCS продолжит работать. Для проверки полной потери DCS нужно остановить или заморозить все три узла либо изолировать их сетью. В этом проекте `pause` надежнее, чем `stop`, потому что контейнеры с политикой restart могут подняться раньше ожидаемого.

Шаг 3. Пока etcd недоступен, проверить поведение:

```powershell
docker compose ps etcd1 etcd2 etcd3
docker compose exec -T patroni1 patronictl -c /etc/patroni/patroni.yml list
curl.exe -i http://127.0.0.1:8008/patroni
curl.exe -i http://127.0.0.1:8009/patroni
curl.exe -i http://127.0.0.1:8010/patroni
```

Проверить доступность через HAProxy:

```powershell
docker compose exec -T -e PGPASSWORD=wasd -e PGCONNECT_TIMEOUT=5 db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d fruits_db -c "SELECT inet_server_addr(), pg_is_in_recovery();"
```

Проверить логи:

```powershell
docker compose logs --tail=120 patroni1 patroni2 patroni3 etcd1 etcd2 etcd3 db workload
```

Ожидаемый результат:

- `patronictl list` может не выполниться, потому что ему тоже нужен DCS.
- В `/patroni` у узлов перестает обновляться `dcs_last_seen`.
- Автоматический failover без DCS невозможен.
- При полной потере etcd текущий primary может демотироваться, чтобы не допустить split-brain.
- На время недоступности DCS возможны ошибки записи или подключения через HAProxy.

Шаг 4. Дождаться возврата etcd и проверить восстановление:

```powershell
docker compose ps
docker compose exec -T etcd1 etcdctl --endpoints=http://etcd1:2379,http://etcd2:2379,http://etcd3:2379 endpoint health
docker compose exec -T patroni1 patronictl -c /etc/patroni/patroni.yml list
docker compose exec -T -e PGPASSWORD=wasd db psql -h 127.0.0.1 -p 5432 -U fruits_admin -d fruits_db -c "INSERT INTO chaos_ha_check DEFAULT VALUES RETURNING id, inet_server_addr(), pg_is_in_recovery(); SELECT count(*) FROM chaos_ha_check;"
```

Ожидаемый результат после возврата:

- Все `etcd` снова healthy.
- `patronictl list` снова показывает кластер.
- Один узел имеет роль `Leader`, остальные - `Replica`.
- Запись через HAProxy снова проходит.
- В Grafana панель `DCS Last Seen Age` возвращается к малым значениям.

Краткий вывод для отчета:

> При полной недоступности etcd Patroni потерял DCS. Без координатора кластер не может безопасно выполнить failover, а текущий primary при невозможности обновлять лидерский lock может прекратить принимать запись. После возврата etcd координация восстановилась: Patroni снова показал роли, был выбран управляемый primary, HAProxy вернул доступ к PostgreSQL.

Фактический прогон в этом проекте:

- Команда `pumba pause --duration 45s fruits_etcd1 fruits_etcd2 fruits_etcd3` перевела все три etcd-контейнера в статус `(Paused)`.
- Во время короткого окна отказа SQL-запись через HAProxy еще успела пройти на текущий primary, но Patroni уже фиксировал проблемы доступа к DCS в логах.
- После возврата etcd все три endpoint снова стали `healthy`.
- После восстановления `patronictl list` показал один `Leader`: `patroni2`, а `patroni1` и `patroni3` стали `Replica`, timeline вырос до `14`.
- Повторная SQL-запись через HAProxy после восстановления прошла успешно, `pg_is_in_recovery()` было `f`.

## Что смотреть в Grafana

Dashboard: `Chaos HA Overview`.

Панели:

- `DB Through HAProxy` - доступность PostgreSQL через HAProxy.
- `Patroni Nodes Running` - сколько PostgreSQL-узлов работает.
- `Current Primary Count` - должен быть ровно `1`.
- `Etcd Members Up` - должно быть `3`, при сценарии 2 падает до `0`.
- `Patroni Primary Role` - видно переключение primary.
- `Patroni Replica Role` - видно, кто является replica.
- `DCS Last Seen Age` - растет, когда Patroni не видит etcd.
- `Replication Streaming` - состояние streaming replication.
- `WAL Replay Lag Bytes` - лаг реплик.
- `Etcd Leader State` - лидерство etcd.
- `HAProxy Patroni Backend Up` - какие backend-ы HAProxy считает доступными.

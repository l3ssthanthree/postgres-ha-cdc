# Этап 6. Оптимизации

## 1. Нагрузка и сервис

Сервис нагрузки находится в `workload/` и запускается отдельным контейнером из `docker-compose.yml`.

Используемые HTTP-эндпоинты:

| Эндпоинт | Профиль | SQL |
| --- | --- | --- |
| `GET /api/oltp/orders/recent` | OLTP чтение | `workload/sql/oltp_recent_orders_v1.sql`, `workload/sql/oltp_recent_orders_v2.sql`, `workload/sql/oltp_recent_orders_v2_optimized.sql` |
| `GET /api/oltp/inventory/availability` | OLTP чтение | `workload/sql/oltp_inventory_availability.sql` |
| `POST /api/oltp/orders/status` | OLTP запись | `workload/sql/oltp_update_order_status.sql` |
| `GET /api/olap/sales/daily` | OLAP | `workload/sql/olap_daily_sales_v1.sql`, `workload/sql/olap_daily_sales_mv.sql` |
| `GET /api/olap/customers/top` | OLAP | `workload/sql/olap_top_customers.sql` |
| `POST /api/logs/events` | Логи | `workload/sql/log_insert_event_batch.sql` |

Требования по профилям выполнены:

- есть не меньше 6 SQL-запросов;
- есть OLTP-чтение с join по 3+ таблицам;
- есть OLTP-запись;
- есть OLAP-агрегации;
- есть непрерывная запись в быстрорастущую таблицу `order_event_log`.

## 2. Конфигурация этапов

### 2.1. Baseline

- версия миграций: `MIGRATION_VERSION=6`
- сиды: `SEED_COUNT=2000`

Размер данных перед прогоном:

- `users`: 8000
- `products`: 10000
- `orders`: 24000
- `order_items`: 72000
- `order_event_log`: 288000

### 2.2. Деградация

Изменения бизнес-логики:

1. `migrations/007_order_risk_flags.sql`
   Добавлена таблица `order_risk_flags` с FK на `orders`.
2. `migrations/008_order_hot_columns.sql`
   Добавлена nullable-колонка `orders.special_instructions`.
3. Усложнен запрос `GET /api/oltp/orders/recent`
   Используется `oltp_recent_orders_v2.sql` с join на `order_risk_flags` и агрегацией по `order_event_log`.

Параметры:

- версия миграций: `MIGRATION_VERSION=8`
- сиды: `SEED_COUNT=5000`

Размер данных перед прогоном:

- `orders`: 60000
- `order_items`: 180000
- `order_event_log`: 720000

Суммарный объем основных таблиц превысил 1 миллион строк. Значение `SEED_COUNT=5000` зафиксировано, потому что деградация уже была хорошо видна.

### 2.3. Оптимизация

Примененные оптимизации:

1. `migrations/009_order_event_log_lookup_index.sql`
   Добавлен индекс `idx_product_price_history_product_id_created_at_desc`.
2. `migrations/010_orders_recent_lookup_index.sql`
   Добавлен индекс `idx_order_event_log_order_id_created_at_desc`.
3. `migrations/011_mv_daily_category_sales.sql`
   Добавлены материализованное представление `mv_daily_category_sales` и индекс `idx_mv_daily_category_sales_day_category`.
4. Переписан SQL для `GET /api/oltp/orders/recent`
   Сначала выбираются только нужные недавние заказы, потом выполняются join и поиск последнего события.
5. Переписан SQL для `GET /api/olap/sales/daily`
   Вместо сырой агрегации используется `mv_daily_category_sales`.

Параметры:

- версия миграций: `MIGRATION_VERSION=11`
- сиды: `SEED_COUNT=5000`

## 3. Профилирование baseline

Артефакты:

- `profiling/1/k6_summary.json`
- `profiling/1/pg_stat_statements.csv`
- `profiling/1/explain/`

Результаты `p95`:

| Эндпоинт | p95, мс |
| --- | ---: |
| `GET /api/oltp/orders/recent` | 417.19 |
| `GET /api/oltp/inventory/availability` | 512.19 |
| `POST /api/oltp/orders/status` | 56.09 |
| `GET /api/olap/sales/daily` | 346.85 |
| `GET /api/olap/customers/top` | 326.72 |
| `POST /api/logs/events` | 54.00 |

Самыми тяжелыми запросами по `pg_stat_statements.mean_exec_time` уже на baseline были inventory, recent orders, daily sales и top customers.

## 4. Наблюдаемая деградация

Артефакты:

- `profiling/2/k6_summary.json`
- `profiling/2/pg_stat_statements.csv`
- `profiling/2/explain/`

Сравнение `p95`:

| Эндпоинт | Пункт 1, мс | Пункт 2, мс | Изменение |
| --- | ---: | ---: | ---: |
| `GET /api/oltp/orders/recent` | 417.19 | 1762.37 | +322.4% |
| `GET /api/oltp/inventory/availability` | 512.19 | 6396.91 | +1148.9% |
| `POST /api/oltp/orders/status` | 56.09 | 63.77 | +13.7% |
| `GET /api/olap/sales/daily` | 346.85 | 494.69 | +42.6% |
| `GET /api/olap/customers/top` | 326.72 | 684.18 | +109.4% |
| `POST /api/logs/events` | 54.00 | 61.70 | +14.3% |

Запросы с ростом `p95` больше чем на 30 процентов:

| Запрос / профиль | Проблема по EXPLAIN / pg_stat | Гипотеза |
| --- | --- | --- |
| `GET /api/oltp/orders/recent` | В `profiling/2/explain/oltp_recent_orders_v2.txt` есть `Seq Scan on order_event_log`, `HashAggregate` и `Sort Method: external merge Disk: 25144kB`. В `pg_stat_statements` среднее время выросло примерно до 1340 мс. | Новый вариант запроса агрегирует почти весь `order_event_log` и добавляет join на `order_risk_flags`, поэтому сильно растут объем сортировки и размер рабочего набора. |
| `GET /api/oltp/inventory/availability` | В `profiling/2/explain/oltp_inventory_availability.txt` видно повторяющийся `Seq Scan on product_price_history pph` при 1111 проходах. Среднее время в `pg_stat_statements` выросло примерно до 5462 мс. | LATERAL-поиск последней цены без составного индекса заставляет PostgreSQL заново сканировать `product_price_history` для каждого найденного товара. |
| `GET /api/olap/sales/daily` | В `profiling/2/explain/olap_daily_sales_v1.txt` остались большие seq scan и внешняя сортировка. | Рост числа строк сделал сырой агрегирующий запрос заметно тяжелее. |
| `GET /api/olap/customers/top` | В `profiling/2/explain/olap_top_customers.txt` есть `GroupAggregate`, `Sort Method: external merge Disk: 10952kB` и seq scan по `order_items` и `orders`. | Запрос остался scan-and-sort, поэтому рост таблиц почти напрямую увеличил его задержку. |

## 5. План оптимизации

| Запрос / профиль | Проблема | Решение | Ожидаемый эффект |
| --- | --- | --- | --- |
| Inventory / OLTP | Повторный seq scan при поиске последней цены | Индекс `(product_id, created_at DESC)` на `product_price_history` | Быстрый поиск последней записи вместо повторных сканов |
| Recent orders / OLTP | Полная агрегация по `order_event_log`, лишний объем сортировки | Индекс `(order_id, created_at DESC)` на `order_event_log` и переписывание SQL | Меньше строк в обработке и быстрый поиск последнего события |
| Daily sales / OLAP | Тяжелая агрегация на лету | Материализованное представление `mv_daily_category_sales` | Чтение предагрегированных данных вместо полной агрегации |

## 6. Результат оптимизаций

Артефакты:

- `profiling/3/k6_write_summary.json`
- `profiling/3/k6_summary.json`
- `profiling/3/pg_stat_statements.csv`
- `profiling/3/explain/`

Что изменилось в планах:

- Для inventory:
  на этапе 2 был `Seq Scan on product_price_history pph`,
  на этапе 3 стал `Index Scan using idx_product_price_history_product_id_created_at_desc on product_price_history pph`.
- Для recent orders:
  на этапе 2 был `Seq Scan on order_event_log el`,
  на этапе 3 стал `Index Only Scan using idx_order_event_log_order_id_created_at_desc on order_event_log el`.
- Для daily sales:
  на этапе 1 был `GroupAggregate` и внешняя сортировка,
  на этапе 3 используется `Index Scan Backward using idx_mv_daily_category_sales_day_category on mv_daily_category_sales`.

Сравнение `p95` после оптимизаций:

| Эндпоинт | Пункт 2, мс | Пункт 3, мс | Изменение |
| --- | ---: | ---: | ---: |
| `GET /api/oltp/orders/recent` | 1762.37 | 117.01 | -93.4% |
| `GET /api/oltp/inventory/availability` | 6396.91 | 122.70 | -98.1% |
| `POST /api/oltp/orders/status` | 63.77 | 62.49 | -2.0% |
| `GET /api/olap/sales/daily` | 494.69 | 38.57 | -92.2% |
| `GET /api/olap/customers/top` | 684.18 | 626.71 | -8.4% |
| `POST /api/logs/events` | 61.70 | 64.10 | +3.9% |

Вывод:

- Основной эффект дали оптимизации recent orders, inventory и daily sales.
- Самым тяжелым запросом в `profiling/3/pg_stat_statements.csv` остался `GET /api/olap/customers/top`, потому что для него не делалась отдельная архитектурная оптимизация.
- Запись после оптимизаций не получила критической деградации.

## 7. Проверка обратного эффекта на запись

Короткий write-only прогон на 1 минуту запускался для:

- `POST /api/oltp/orders/status`
- `POST /api/logs/events`

Сравнение `p95`:

| Метрика | Baseline, мс | Пункт 3 write-only, мс | Изменение |
| --- | ---: | ---: | ---: |
| `POST /api/oltp/orders/status` | 56.09 | 48.68 | -13.2% |
| `POST /api/logs/events` | 54.00 | 48.97 | -9.3% |

Сравнение server-side EXPLAIN:

- `profiling/1/explain/oltp_update_order_status.txt`: около 0.844 мс
- `profiling/3/explain/oltp_update_order_status.txt`: около 1.339 мс
- `profiling/1/explain/log_insert_event_batch.txt`: около 2.747 мс
- `profiling/3/explain/log_insert_event_batch.txt`: около 4.125 мс

Вывод по записи:

- PostgreSQL действительно делает немного больше работы на запись после добавления индексов.
- На уровне k6 это не привело к заметной деградации write-only сценария.
- Требование про проверку обратного эффекта оптимизаций на `INSERT` и `UPDATE` выполнено.

## 8. Grafana

Файлы:

- `grafana/dashboards/optimization.json`
- `grafana/provisioning/datasources/datasource.yml`
- `grafana/provisioning/dashboards/dashboards.yml`

На дашборде есть:

- Top-5 long queries по `pg_stat_statements`
- Cache Hit Ratio
- Active connections
- Idle connections

Автоподнятие проверено через Grafana API:

- есть папка `Optimization`
- есть дашборд `PostgreSQL Optimization Overview`
- есть datasource `Prometheus`
- есть datasource `PostgreSQL`

## 9. Итоговая сводная таблица

| Запрос / Эндпоинт | p95 пункт 1, мс | p95 пункт 2, мс | p95 пункт 3, мс | 1->2 | 2->3 | Примененное решение |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| `GET /api/oltp/orders/recent` | 417.19 | 1762.37 | 117.01 | +322.4% | -93.4% | Переписывание SQL и индекс на `order_event_log(order_id, created_at DESC)` |
| `GET /api/oltp/inventory/availability` | 512.19 | 6396.91 | 122.70 | +1148.9% | -98.1% | Индекс на `product_price_history(product_id, created_at DESC)` |
| `POST /api/oltp/orders/status` | 56.09 | 63.77 | 62.49 | +13.7% | -2.0% | Отдельная оптимизация не применялась, задержка осталась стабильной |
| `GET /api/olap/sales/daily` | 346.85 | 494.69 | 38.57 | +42.6% | -92.2% | Материализованное представление `mv_daily_category_sales` |
| `GET /api/olap/customers/top` | 326.72 | 684.18 | 626.71 | +109.4% | -8.4% | Косвенный эффект от общих оптимизаций |
| `POST /api/logs/events` | 54.00 | 61.70 | 64.10 | +14.3% | +3.9% | Прямая оптимизация не применялась, рост допустим |

## 10. Чеклист

- Workload-сервис с 6+ запросами по 3 профилям запускается в Docker
- `pg_stat_statements` включен миграцией
- Профилирование сохранено в `profiling/1`, `profiling/2`, `profiling/3`
- В пункте 2 есть 2 изменения схемы и усложнение запроса
- Деградация в пункте 2 подтверждена метриками и объяснена через EXPLAIN
- Применены минимум 2 индекса и 1 архитектурная техника
- Проверен обратный эффект оптимизаций на `INSERT` и `UPDATE`
- Grafana-дашборд поднимается автоматически из JSON
- Итоговая сводная таблица есть в этом файле

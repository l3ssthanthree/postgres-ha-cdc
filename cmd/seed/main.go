package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	seedUserEmailPrefix = "seed_user_"
	defaultSeedCount    = 10
	defaultAppEnv       = "dev"
	defaultDBPingTO     = 5 * time.Second
)

type config struct {
	AppEnv    string
	DSN       string
	SeedCount int
}

type seeder struct {
	db  *sql.DB
	cfg config
}

type stage struct {
	name   string
	tables []string
	run    func(context.Context) error
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if strings.EqualFold(cfg.AppEnv, "prod") {
		log.Println("APP_ENV=prod, seeding is disabled")
		return
	}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), defaultDBPingTO)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	s := &seeder{
		db:  db,
		cfg: cfg,
	}

	log.Printf("starting stage-aware seeding, env=%s, seed_count=%d", cfg.AppEnv, cfg.SeedCount)

	if err := s.run(context.Background()); err != nil {
		log.Fatalf("seeding failed: %v", err)
	}

	log.Println("seeding completed successfully")
}

func loadConfig() (config, error) {
	cfg := config{
		AppEnv:    getEnv("APP_ENV", defaultAppEnv),
		SeedCount: defaultSeedCount,
		DSN:       os.Getenv("GOOSE_DBSTRING_DOCKER"),
	}

	if cfg.DSN == "" {
		return config{}, errors.New("GOOSE_DBSTRING_DOCKER is required")
	}

	if raw := os.Getenv("SEED_COUNT"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			return config{}, fmt.Errorf("invalid SEED_COUNT: %w", err)
		}
		if v <= 0 {
			return config{}, errors.New("SEED_COUNT must be greater than 0")
		}
		cfg.SeedCount = v
	}

	return cfg, nil
}

func (s *seeder) run(ctx context.Context) error {
	if err := s.resetData(ctx); err != nil {
		return fmt.Errorf("reset data: %w", err)
	}

	stages := []stage{
		{
			name:   "v1_users_and_addresses",
			tables: []string{"users", "addresses"},
			run:    s.seedV1,
		},
		{
			name: "v2_catalog_and_inventory",
			tables: []string{
				"suppliers",
				"categories",
				"products",
				"supplier_products",
				"product_price_history",
				"warehouses",
				"warehouse_stock",
				"supplier_stock",
			},
			run: s.seedV2,
		},
		{
			name:   "v3_orders",
			tables: []string{"order_statuses", "orders", "order_items"},
			run:    s.seedV3,
		},
		{
			name:   "v4_payments",
			tables: []string{"payment_statuses", "payments"},
			run:    s.seedV4,
		},
		{
			name:   "v5_deliveries",
			tables: []string{"delivery_statuses", "deliveries"},
			run:    s.seedV5,
		},
		{
			name:   "v6_workload_log",
			tables: []string{"order_event_log"},
			run:    s.seedV6,
		},
		{
			name:   "v7_order_risk_flags",
			tables: []string{"order_risk_flags"},
			run:    s.seedV7,
		},
	}

	for _, st := range stages {
		ok, err := allTablesExist(ctx, s.db, st.tables...)
		if err != nil {
			return fmt.Errorf("check tables for stage %s: %w", st.name, err)
		}
		if !ok {
			log.Printf("skip stage %s: required tables do not exist", st.name)
			continue
		}

		log.Printf("running stage %s", st.name)

		if err := st.run(ctx); err != nil {
			return fmt.Errorf("stage %s: %w", st.name, err)
		}

		log.Printf("stage %s completed", st.name)
	}

	if err := s.seedHotColumns(ctx); err != nil {
		return fmt.Errorf("seed hot columns: %w", err)
	}

	if err := s.refreshMaterializedViews(ctx); err != nil {
		return fmt.Errorf("refresh materialized views: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `ANALYZE`); err != nil {
		return fmt.Errorf("analyze: %w", err)
	}

	return nil
}

func (s *seeder) resetData(ctx context.Context) error {
	candidates := []string{
		"order_event_log",
		"order_risk_flags",
		"deliveries",
		"delivery_statuses",
		"payments",
		"payment_statuses",
		"order_items",
		"orders",
		"order_statuses",
		"warehouse_stock",
		"supplier_stock",
		"supplier_products",
		"product_price_history",
		"products",
		"warehouses",
		"categories",
		"suppliers",
		"addresses",
		"users",
	}

	var existing []string
	for _, table := range candidates {
		ok, err := tableExists(ctx, s.db, table)
		if err != nil {
			return fmt.Errorf("check table %s: %w", table, err)
		}
		if ok {
			existing = append(existing, table)
		}
	}

	if len(existing) == 0 {
		return nil
	}

	query := "TRUNCATE TABLE " + strings.Join(existing, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("truncate seed tables: %w", err)
	}

	return nil
}

func (s *seeder) seedV1(ctx context.Context) error {
	targetUsers := 4 * s.cfg.SeedCount

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO users (full_name, email, phone, created_at)
		SELECT
			format('Seed User %s', gs),
			format('seed_user_%s@example.com', gs),
			'+100000' || lpad(gs::text, 5, '0'),
			NOW() - make_interval(days => gs % 365, secs => (gs * 37) % 86400)
		FROM generate_series(1, $1) AS gs
	`, targetUsers); err != nil {
		return fmt.Errorf("insert users: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO addresses (user_id, city, street, postal_code)
		SELECT
			u.id,
			format('City %s', ((u.id - 1) % 50) + 1),
			format('Street %s', u.id),
			lpad((((u.id * 97) % 99999) + 1)::text, 5, '0')
		FROM users u
		ORDER BY u.id
	`); err != nil {
		return fmt.Errorf("insert addresses: %w", err)
	}

	log.Printf("v1 seeded: users=%d", targetUsers)
	return nil
}

func (s *seeder) seedV2(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO categories (name)
		VALUES
			('Citrus'),
			('Berries'),
			('Tropical'),
			('Stone Fruits'),
			('Pome Fruits')
	`); err != nil {
		return fmt.Errorf("insert categories: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO warehouses (name)
		VALUES
			('Main Warehouse'),
			('North Warehouse'),
			('Reserve Warehouse')
	`); err != nil {
		return fmt.Errorf("insert warehouses: %w", err)
	}

	targetSuppliers := max(12, s.cfg.SeedCount)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO suppliers (name, contact_phone, created_at)
		SELECT
			format('Supplier %s', gs),
			'+200000' || lpad(gs::text, 5, '0'),
			NOW() - make_interval(days => gs % 365, secs => (gs * 19) % 86400)
		FROM generate_series(1, $1) AS gs
	`, targetSuppliers); err != nil {
		return fmt.Errorf("insert suppliers: %w", err)
	}

	targetProducts := 5 * s.cfg.SeedCount
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO products (category_id, name, description, unit, created_at)
		SELECT
			((gs - 1) % 5) + 1,
			format('Fruit %s', gs),
			format('Synthetic fruit product %s for workload profiling', gs),
			'kg',
			NOW() - make_interval(days => (gs * 3) % 365, secs => (gs * 11) % 86400)
		FROM generate_series(1, $1) AS gs
	`, targetProducts); err != nil {
		return fmt.Errorf("insert products: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO product_price_history (product_id, price, created_at)
		SELECT
			p.id,
			round((25 + ((p.id * hist.n) % 180))::numeric, 2),
			p.created_at + make_interval(days => hist.n)
		FROM products p
		CROSS JOIN generate_series(1, 3) AS hist(n)
	`); err != nil {
		return fmt.Errorf("insert product price history: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO supplier_products (supplier_id, product_id, purchase_price, created_at)
		SELECT
			((p.id - 1) % $1) + 1,
			p.id,
			round((10 + ((p.id * 7) % 90))::numeric, 2),
			p.created_at
		FROM products p
	`, targetSuppliers); err != nil {
		return fmt.Errorf("insert supplier_products primary supplier: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO supplier_products (supplier_id, product_id, purchase_price, created_at)
		SELECT
			((p.id + 16) % $1) + 1,
			p.id,
			round((12 + ((p.id * 13) % 110))::numeric, 2),
			p.created_at + INTERVAL '1 hour'
		FROM products p
		WHERE p.id % 4 = 0
	`, targetSuppliers); err != nil {
		return fmt.Errorf("insert supplier_products secondary supplier: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO supplier_stock (supplier_product_id, qty_available, updated_at)
		SELECT
			sp.id,
			50 + ((sp.id * 7) % 400),
			sp.created_at + INTERVAL '2 hours'
		FROM supplier_products sp
	`); err != nil {
		return fmt.Errorf("insert supplier_stock: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO warehouse_stock (warehouse_id, product_id, qty_available)
		SELECT
			w.id,
			p.id,
			10 + ((p.id * w.id) % 150)
		FROM warehouses w
		CROSS JOIN products p
	`); err != nil {
		return fmt.Errorf("insert warehouse_stock: %w", err)
	}

	log.Printf("v2 seeded: suppliers=%d, products=%d", targetSuppliers, targetProducts)
	return nil
}

func (s *seeder) seedV3(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO order_statuses (name)
		VALUES
			('new'),
			('paid'),
			('processing'),
			('shipped'),
			('delivered'),
			('cancelled')
	`); err != nil {
		return fmt.Errorf("insert order statuses: %w", err)
	}

	targetUsers := 4 * s.cfg.SeedCount
	targetOrders := 12 * s.cfg.SeedCount
	targetProducts := 5 * s.cfg.SeedCount

	if _, err := s.db.ExecContext(ctx, `
		WITH generated_orders AS (
			SELECT
				gs,
				((gs - 1) % $1) + 1 AS user_id,
				CASE
					WHEN gs % 17 = 0 THEN 'cancelled'
					WHEN gs % 13 = 0 THEN 'delivered'
					WHEN gs % 11 = 0 THEN 'shipped'
					WHEN gs % 7 = 0 THEN 'processing'
					WHEN gs % 5 = 0 THEN 'paid'
					ELSE 'new'
				END AS status_name,
				NOW() - make_interval(days => (gs * 5) % 365, secs => (gs * 29) % 86400) AS created_at
			FROM generate_series(1, $2) AS gs
		)
		INSERT INTO orders (user_id, status_id, created_at)
		SELECT
			go.user_id,
			os.id,
			go.created_at
		FROM generated_orders go
		JOIN order_statuses os ON os.name = go.status_name
		ORDER BY go.gs
	`, targetUsers, targetOrders); err != nil {
		return fmt.Errorf("insert orders: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO order_items (order_id, product_id, quantity, sale_price_at_purchase)
		SELECT
			o.id,
			(((o.id * item.n * 13) - 1) % $1) + 1,
			1 + ((o.id + item.n) % 5),
			round((30 + ((o.id * item.n) % 220))::numeric, 2)
		FROM orders o
		CROSS JOIN generate_series(1, 3) AS item(n)
		ORDER BY o.id, item.n
	`, targetProducts); err != nil {
		return fmt.Errorf("insert order_items: %w", err)
	}

	log.Printf("v3 seeded: orders=%d", targetOrders)
	return nil
}

func (s *seeder) seedV4(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO payment_statuses (name)
		VALUES
			('pending'),
			('paid'),
			('failed'),
			('refunded')
	`); err != nil {
		return fmt.Errorf("insert payment statuses: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		WITH order_totals AS (
			SELECT
				o.id AS order_id,
				o.created_at,
				os.name AS order_status_name,
				SUM(oi.quantity * oi.sale_price_at_purchase) AS amount
			FROM orders o
			JOIN order_statuses os ON os.id = o.status_id
			JOIN order_items oi ON oi.order_id = o.id
			GROUP BY o.id, o.created_at, os.name
		)
		INSERT INTO payments (order_id, status_id, amount, paid_at)
		SELECT
			ot.order_id,
			ps.id,
			ot.amount,
			CASE
				WHEN ps.name = 'pending' THEN NULL
				ELSE ot.created_at + INTERVAL '2 hours'
			END
		FROM order_totals ot
		JOIN payment_statuses ps
			ON ps.name = CASE
				WHEN ot.order_status_name = 'cancelled' THEN 'refunded'
				WHEN ot.order_status_name IN ('new', 'processing') THEN 'pending'
				ELSE 'paid'
			END
	`); err != nil {
		return fmt.Errorf("insert payments: %w", err)
	}

	log.Printf("v4 seeded: payments=%d", 12*s.cfg.SeedCount)
	return nil
}

func (s *seeder) seedV5(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO delivery_statuses (name)
		VALUES
			('pending'),
			('packed'),
			('in_transit'),
			('delivered'),
			('returned')
	`); err != nil {
		return fmt.Errorf("insert delivery statuses: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		WITH user_primary_address AS (
			SELECT
				a.user_id,
				MIN(a.id) AS address_id
			FROM addresses a
			GROUP BY a.user_id
		)
		INSERT INTO deliveries (order_id, address_id, status_id)
		SELECT
			o.id,
			upa.address_id,
			ds.id
		FROM orders o
		JOIN order_statuses os ON os.id = o.status_id
		JOIN user_primary_address upa ON upa.user_id = o.user_id
		JOIN delivery_statuses ds
			ON ds.name = CASE
				WHEN os.name = 'delivered' THEN 'delivered'
				WHEN os.name = 'shipped' THEN 'in_transit'
				WHEN os.name = 'processing' THEN 'packed'
				WHEN os.name = 'cancelled' THEN 'returned'
				ELSE 'pending'
			END
	`); err != nil {
		return fmt.Errorf("insert deliveries: %w", err)
	}

	log.Printf("v5 seeded: deliveries=%d", 12*s.cfg.SeedCount)
	return nil
}

func (s *seeder) seedV6(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO order_event_log (order_id, event_type, actor_type, payload, created_at)
		SELECT
			o.id,
			CASE
				WHEN ev.n % 6 = 0 THEN 'inventory_check'
				WHEN ev.n % 5 = 0 THEN 'payment_sync'
				WHEN ev.n % 4 = 0 THEN 'fraud_scan'
				WHEN ev.n % 3 = 0 THEN 'delivery_update'
				WHEN ev.n % 2 = 0 THEN 'status_transition'
				ELSE 'user_view'
			END,
			CASE
				WHEN ev.n % 4 = 0 THEN 'system'
				WHEN ev.n % 3 = 0 THEN 'worker'
				ELSE 'user'
			END,
			jsonb_build_object(
				'step', ev.n,
				'order_id', o.id,
				'status_id', o.status_id
			),
			o.created_at + make_interval(mins => ev.n * 5)
		FROM orders o
		CROSS JOIN generate_series(1, 12) AS ev(n)
		ORDER BY o.id, ev.n
	`); err != nil {
		return fmt.Errorf("insert order_event_log: %w", err)
	}

	log.Printf("v6 seeded: order_event_log=%d", 144*s.cfg.SeedCount)
	return nil
}

func (s *seeder) seedV7(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO order_risk_flags (order_id, flag_code, severity, created_at)
		SELECT
			o.id,
			CASE flag.n
				WHEN 1 THEN 'high_amount'
				WHEN 2 THEN 'address_change'
				ELSE 'many_events'
			END,
			1 + ((o.id + flag.n) % 5),
			o.created_at + make_interval(mins => flag.n * 15)
		FROM orders o
		JOIN generate_series(1, 3) AS flag(n)
			ON (
				(flag.n = 1 AND o.id % 7 = 0) OR
				(flag.n = 2 AND o.id % 11 = 0) OR
				(flag.n = 3 AND o.id % 13 = 0)
			)
		ORDER BY o.id, flag.n
	`); err != nil {
		return fmt.Errorf("insert order_risk_flags: %w", err)
	}

	log.Printf("v7 seeded: order_risk_flags for degraded profile")
	return nil
}

func (s *seeder) seedHotColumns(ctx context.Context) error {
	ok, err := columnExists(ctx, s.db, "orders", "special_instructions")
	if err != nil {
		return err
	}
	if !ok {
		log.Printf("skip hot column seed: orders.special_instructions does not exist")
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE orders
		SET special_instructions = CASE
			WHEN id % 4 = 0 THEN format('Handle order %s carefully; gate code %s; customer requested extra packaging.', id, (id * 17) % 10000)
			WHEN id % 9 = 0 THEN format('Deliver order %s after 18:00 and call before arrival.', id)
			ELSE NULL
		END
	`); err != nil {
		return fmt.Errorf("update hot order column: %w", err)
	}

	log.Printf("optional stage seeded: orders.special_instructions")
	return nil
}

func (s *seeder) refreshMaterializedViews(ctx context.Context) error {
	ok, err := materializedViewExists(ctx, s.db, "mv_daily_category_sales")
	if err != nil {
		return err
	}
	if !ok {
		log.Printf("skip materialized view refresh: mv_daily_category_sales does not exist")
		return nil
	}

	if _, err := s.db.ExecContext(ctx, `REFRESH MATERIALIZED VIEW mv_daily_category_sales`); err != nil {
		return fmt.Errorf("refresh mv_daily_category_sales: %w", err)
	}

	log.Printf("refreshed materialized view mv_daily_category_sales")
	return nil
}

func tableExists(ctx context.Context, db queryRower, tableName string) (bool, error) {
	var exists bool

	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = $1
		)
	`, tableName).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func materializedViewExists(ctx context.Context, db queryRower, viewName string) (bool, error) {
	var exists bool

	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_matviews
			WHERE schemaname = 'public'
			  AND matviewname = $1
		)
	`, viewName).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func columnExists(ctx context.Context, db queryRower, tableName, columnName string) (bool, error) {
	var exists bool

	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = $1
			  AND column_name = $2
		)
	`, tableName, columnName).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func allTablesExist(ctx context.Context, db queryRower, tables ...string) (bool, error) {
	for _, table := range tables {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

func getEnv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

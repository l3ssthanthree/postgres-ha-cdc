package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/brianvoe/gofakeit/v7"
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
	rng *rand.Rand
}

type stage struct {
	name   string
	tables []string
	run    func(context.Context) error
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type dbtx interface {
	queryRower
	queryer
	execer
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

	seed := int64(42)
	src := rand.New(rand.NewSource(seed))

	s := &seeder{
		db:  db,
		cfg: cfg,
		rng: src,
	}

	gofakeit.Seed(seed)

	log.Printf("starting seeding, env=%s, seed_count=%d", cfg.AppEnv, cfg.SeedCount)

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

	return nil
}

func (s *seeder) seedV1(ctx context.Context) error {
	targetUsers := 5 * s.cfg.SeedCount

	for i := 1; i <= targetUsers; i++ {
		fullName := gofakeit.Name()
		email := fmt.Sprintf("%s%d@example.com", seedUserEmailPrefix, i)
		phone := normalizePhone(gofakeit.Phone())

		var userID int64
		if err := s.db.QueryRowContext(ctx, `
			INSERT INTO users (full_name, email, phone)
			VALUES ($1, $2, $3)
			ON CONFLICT (email) DO UPDATE
			SET full_name = EXCLUDED.full_name,
			    phone = EXCLUDED.phone
			RETURNING id
		`, fullName, email, phone).Scan(&userID); err != nil {
			return fmt.Errorf("upsert user %s: %w", email, err)
		}

		city := gofakeit.City()
		street := gofakeit.Street()
		postalCode := gofakeit.Zip()

		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO addresses (user_id, city, street, postal_code)
			SELECT $1, $2, $3, $4
			WHERE NOT EXISTS (
				SELECT 1
				FROM addresses
				WHERE user_id = $1
			)
		`, userID, city, street, postalCode); err != nil {
			return fmt.Errorf("insert address for user_id=%d: %w", userID, err)
		}
	}

	log.Printf("v1 seeded: users=%d", targetUsers)
	return nil
}

func (s *seeder) seedV2(ctx context.Context) error {
	categories := []string{
		"Citrus",
		"Berries",
		"Tropical",
		"Stone Fruits",
		"Pome Fruits",
	}
	for _, name := range categories {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO categories (name)
			VALUES ($1)
			ON CONFLICT (name) DO NOTHING
		`, name); err != nil {
			return fmt.Errorf("insert category %q: %w", name, err)
		}
	}

	warehouses := []string{
		"Main Warehouse",
		"Reserve Warehouse",
	}
	for _, name := range warehouses {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO warehouses (name)
			VALUES ($1)
			ON CONFLICT (name) DO NOTHING
		`, name); err != nil {
			return fmt.Errorf("insert warehouse %q: %w", name, err)
		}
	}

	targetSuppliers := 2 * s.cfg.SeedCount
	for i := 1; i <= targetSuppliers; i++ {
		name := fmt.Sprintf("%s %s %d", gofakeit.Company(), gofakeit.CompanySuffix(), i)
		phone := normalizePhone(gofakeit.Phone())

		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO suppliers (name, contact_phone)
			SELECT v.name, v.contact_phone
			FROM (VALUES ($1::varchar, $2::varchar)) AS v(name, contact_phone)
			WHERE NOT EXISTS (
				SELECT 1
				FROM suppliers
				WHERE name = v.name
			)
		`, name, phone); err != nil {
			return fmt.Errorf("insert supplier %q: %w", name, err)
		}
	}

	categoryIDs, err := fetchIDs(ctx, s.db, `SELECT id FROM categories ORDER BY id`)
	if err != nil {
		return fmt.Errorf("fetch category ids: %w", err)
	}
	if len(categoryIDs) == 0 {
		return errors.New("no categories found after seeding")
	}

	targetProducts := 10 * s.cfg.SeedCount
	for i := 1; i <= targetProducts; i++ {
		categoryID := categoryIDs[(i-1)%len(categoryIDs)]
		name := fmt.Sprintf("%s %d", gofakeit.Fruit(), i)
		description := fmt.Sprintf(
			"%s. %s",
			gofakeit.Sentence(8),
			gofakeit.Sentence(12),
		)

		var productID int64
		if err := s.db.QueryRowContext(ctx, `
			WITH inserted AS (
				INSERT INTO products (category_id, name, description, unit)
				SELECT $1, v.name, $3, 'kg'
				FROM (VALUES ($2::varchar)) AS v(name)
				WHERE NOT EXISTS (
					SELECT 1
					FROM products
					WHERE name = v.name
				)
				RETURNING id
			)
			SELECT id FROM inserted
			UNION ALL
			SELECT id FROM products WHERE name = $2::varchar
			LIMIT 1
		`, categoryID, name, description).Scan(&productID); err != nil {
			return fmt.Errorf("upsert-like product %q: %w", name, err)
		}

		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO product_price_history (product_id, price)
			SELECT $1, $2
			WHERE NOT EXISTS (
				SELECT 1
				FROM product_price_history
				WHERE product_id = $1
			)
		`, productID, randomPrice(s.rng, 50, 250)); err != nil {
			return fmt.Errorf("insert price history for product_id=%d: %w", productID, err)
		}
	}

	if err := s.seedStocks(ctx); err != nil {
		return fmt.Errorf("seed stocks: %w", err)
	}

	log.Printf("v2 seeded: suppliers=%d, products=%d", targetSuppliers, targetProducts)
	return nil
}

func (s *seeder) seedStocks(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO supplier_products (supplier_id, product_id, purchase_price)
		SELECT
			s.id,
			p.id,
			round((20 + random() * 100)::numeric, 2)
		FROM products p
		JOIN LATERAL (
			SELECT id
			FROM suppliers
			ORDER BY id
			OFFSET ((p.id - 1) % GREATEST((SELECT COUNT(*) FROM suppliers), 1))
			LIMIT 1
		) s ON true
		ON CONFLICT (supplier_id, product_id) DO NOTHING
	`); err != nil {
		return fmt.Errorf("insert supplier_products: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO supplier_stock (supplier_product_id, qty_available)
		SELECT
			sp.id,
			50 + ((sp.id * 7) % 100)
		FROM supplier_products sp
		ON CONFLICT (supplier_product_id) DO NOTHING
	`); err != nil {
		return fmt.Errorf("insert supplier_stock: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO warehouse_stock (warehouse_id, product_id, qty_available)
		SELECT
			w.id,
			p.id,
			10 + ((p.id * w.id) % 40)
		FROM warehouses w
		CROSS JOIN products p
		ON CONFLICT (warehouse_id, product_id) DO NOTHING
	`); err != nil {
		return fmt.Errorf("insert warehouse_stock: %w", err)
	}

	return nil
}

func (s *seeder) seedV3(ctx context.Context) error {
	statuses := []string{"new", "paid", "processing", "shipped", "delivered", "cancelled"}
	for _, name := range statuses {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO order_statuses (name)
			VALUES ($1)
			ON CONFLICT (name) DO NOTHING
		`, name); err != nil {
			return fmt.Errorf("insert order status %q: %w", name, err)
		}
	}

	newStatusID, err := getStatusIDByName(ctx, s.db, "order_statuses", "new")
	if err != nil {
		return err
	}

	if err := deleteSeedOrdersCascade(ctx, s.db); err != nil {
		return fmt.Errorf("delete old seed orders cascade: %w", err)
	}

	userIDs, err := fetchIDs(ctx, s.db, `
		SELECT id
		FROM users
		WHERE email LIKE $1
		ORDER BY id
	`, seedUserEmailPrefix+"%")
	if err != nil {
		return fmt.Errorf("fetch seed user ids: %w", err)
	}
	if len(userIDs) == 0 {
		return errors.New("no seed users found")
	}

	productIDs, err := fetchIDs(ctx, s.db, `
	SELECT id
	FROM products
	ORDER BY id
`)
	if err != nil {
		return fmt.Errorf("fetch product ids: %w", err)
	}
	if len(productIDs) == 0 {
		return errors.New("no products found")
	}

	targetOrders := 20 * s.cfg.SeedCount
	for i := 1; i <= targetOrders; i++ {
		userID := userIDs[(i-1)%len(userIDs)]

		var orderID int64
		if err := s.db.QueryRowContext(ctx, `
			INSERT INTO orders (user_id, status_id)
			VALUES ($1, $2)
			RETURNING id
		`, userID, newStatusID).Scan(&orderID); err != nil {
			return fmt.Errorf("insert order #%d: %w", i, err)
		}

		itemsCount := 1 + s.rng.Intn(3)
		for j := 0; j < itemsCount; j++ {
			productID := productIDs[(i+j-1+len(productIDs))%len(productIDs)]
			quantity := 1 + s.rng.Intn(5)
			price := randomPrice(s.rng, 70, 300)

			if _, err := s.db.ExecContext(ctx, `
				INSERT INTO order_items (order_id, product_id, quantity, sale_price_at_purchase)
				VALUES ($1, $2, $3, $4)
			`, orderID, productID, quantity, price); err != nil {
				return fmt.Errorf("insert order item for order_id=%d: %w", orderID, err)
			}
		}
	}

	log.Printf("v3 seeded: orders=%d", targetOrders)
	return nil
}

func (s *seeder) seedV4(ctx context.Context) error {
	statuses := []string{"pending", "paid", "failed", "refunded"}
	for _, name := range statuses {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO payment_statuses (name)
			VALUES ($1)
			ON CONFLICT (name) DO NOTHING
		`, name); err != nil {
			return fmt.Errorf("insert payment status %q: %w", name, err)
		}
	}

	pendingStatusID, err := getStatusIDByName(ctx, s.db, "payment_statuses", "pending")
	if err != nil {
		return err
	}

	orderIDs, err := fetchIDs(ctx, s.db, `
		SELECT o.id
		FROM orders o
		JOIN users u ON u.id = o.user_id
		WHERE u.email LIKE $1
		ORDER BY o.id
	`, seedUserEmailPrefix+"%")
	if err != nil {
		return fmt.Errorf("fetch seed order ids for payments: %w", err)
	}

	for _, orderID := range orderIDs {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO payments (order_id, status_id, amount, paid_at)
			SELECT $1, $2, $3, NULL
			WHERE NOT EXISTS (
				SELECT 1
				FROM payments
				WHERE order_id = $1
			)
		`, orderID, pendingStatusID, randomPrice(s.rng, 100, 800)); err != nil {
			return fmt.Errorf("insert payment for order_id=%d: %w", orderID, err)
		}
	}

	log.Printf("v4 seeded: payments for orders=%d", len(orderIDs))
	return nil
}

func (s *seeder) seedV5(ctx context.Context) error {
	statuses := []string{"pending", "packed", "in_transit", "delivered", "returned"}
	for _, name := range statuses {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO delivery_statuses (name)
			VALUES ($1)
			ON CONFLICT (name) DO NOTHING
		`, name); err != nil {
			return fmt.Errorf("insert delivery status %q: %w", name, err)
		}
	}

	pendingStatusID, err := getStatusIDByName(ctx, s.db, "delivery_statuses", "pending")
	if err != nil {
		return err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT o.id, a.id
		FROM orders o
		JOIN users u ON u.id = o.user_id
		JOIN addresses a ON a.user_id = u.id
		WHERE u.email LIKE $1
		ORDER BY o.id
	`, seedUserEmailPrefix+"%")
	if err != nil {
		return fmt.Errorf("fetch seed order/address pairs for deliveries: %w", err)
	}
	defer rows.Close()

	type pair struct {
		orderID   int64
		addressID int64
	}

	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.orderID, &p.addressID); err != nil {
			return fmt.Errorf("scan order/address pair: %w", err)
		}
		pairs = append(pairs, p)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate order/address pairs: %w", err)
	}

	for _, p := range pairs {
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO deliveries (order_id, address_id, status_id)
			SELECT $1, $2, $3
			WHERE NOT EXISTS (
				SELECT 1
				FROM deliveries
				WHERE order_id = $1
			)
		`, p.orderID, p.addressID, pendingStatusID); err != nil {
			return fmt.Errorf("insert delivery for order_id=%d: %w", p.orderID, err)
		}
	}

	log.Printf("v5 seeded: deliveries=%d", len(pairs))
	return nil
}

func deleteSeedOrdersCascade(ctx context.Context, db dbtx) error {
	seedUserIDsSubquery := `
		SELECT id
		FROM users
		WHERE email LIKE $1
	`

	if _, err := db.ExecContext(ctx, `
		DELETE FROM order_items
		WHERE order_id IN (
			SELECT id
			FROM orders
			WHERE user_id IN (`+seedUserIDsSubquery+`)
		)
	`, seedUserEmailPrefix+"%"); err != nil {
		return fmt.Errorf("delete order_items: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		DELETE FROM payments
		WHERE order_id IN (
			SELECT id
			FROM orders
			WHERE user_id IN (`+seedUserIDsSubquery+`)
		)
	`, seedUserEmailPrefix+"%"); err != nil {
		return fmt.Errorf("delete payments: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		DELETE FROM deliveries
		WHERE order_id IN (
			SELECT id
			FROM orders
			WHERE user_id IN (`+seedUserIDsSubquery+`)
		)
	`, seedUserEmailPrefix+"%"); err != nil {
		return fmt.Errorf("delete deliveries: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		DELETE FROM orders
		WHERE user_id IN (`+seedUserIDsSubquery+`)
	`, seedUserEmailPrefix+"%"); err != nil {
		return fmt.Errorf("delete orders: %w", err)
	}

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

func fetchIDs(ctx context.Context, db queryer, query string, args ...any) ([]int64, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return ids, nil
}

func getStatusIDByName(ctx context.Context, db queryRower, tableName, statusName string) (int64, error) {
	query := fmt.Sprintf(`SELECT id FROM %s WHERE name = $1`, tableName)

	var id int64
	if err := db.QueryRowContext(ctx, query, statusName).Scan(&id); err != nil {
		return 0, fmt.Errorf("get status id from %s by name %q: %w", tableName, statusName, err)
	}

	return id, nil
}

func randomPrice(rng *rand.Rand, min, max float64) float64 {
	if max <= min {
		return min
	}
	value := min + rng.Float64()*(max-min)
	return float64(int(value*100)) / 100
}

func normalizePhone(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "+10000000000"
	}
	return v
}

func getEnv(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

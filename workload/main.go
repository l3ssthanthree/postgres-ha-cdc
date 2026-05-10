package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed sql/*.sql
var sqlFS embed.FS

const (
	defaultWorkloadPort   = "8080"
	defaultQueryTimeout   = 60 * time.Second
	defaultServerTimeout  = 75 * time.Second
	defaultRecentDays     = 365
	defaultInventoryTerm  = "%Fruit 12%"
	defaultInventoryLimit = 25
	defaultRecentLimit    = 25
	defaultTopCustomers   = 20
	defaultLogBatchSize   = 40
)

type config struct {
	Port string
	DSN  string
}

type app struct {
	db             *sql.DB
	cfg            config
	queries        queries
	caps           capabilities
	orderCount     int64
	eventSeed      atomic.Int64
	orderCursor    atomic.Int64
	requestTimeout time.Duration
}

type capabilities struct {
	HasRiskFlags           bool `json:"has_risk_flags"`
	HasSpecialInstructions bool `json:"has_special_instructions"`
	HasDailySalesMV        bool `json:"has_daily_sales_mv"`
}

type queries struct {
	RecentOrdersV1         string
	RecentOrdersV2Degraded string
	RecentOrdersV2Fast     string
	Inventory              string
	UpdateStatus           string
	LogBatchInsert         string
	DailySalesV1           string
	DailySalesMV           string
	TopCustomers           string
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := sql.Open("pgx", cfg.DSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(40)
	db.SetMaxIdleConns(20)
	db.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), defaultQueryTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	queries, err := loadQueries()
	if err != nil {
		log.Fatalf("load queries: %v", err)
	}

	caps, err := detectCapabilities(ctx, db)
	if err != nil {
		log.Fatalf("detect capabilities: %v", err)
	}

	orderCount, err := fetchOrderCount(ctx, db)
	if err != nil {
		log.Fatalf("fetch order count: %v", err)
	}
	if orderCount == 0 {
		log.Fatalf("orders table is empty; run migrations and seeds first")
	}

	app := &app{
		db:             db,
		cfg:            cfg,
		queries:        queries,
		caps:           caps,
		orderCount:     orderCount,
		requestTimeout: defaultQueryTimeout,
	}

	log.Printf(
		"workload service started on :%s (orders=%d, risk_flags=%t, special_instructions=%t, mv_daily_sales=%t)",
		cfg.Port,
		orderCount,
		caps.HasRiskFlags,
		caps.HasSpecialInstructions,
		caps.HasDailySalesMV,
	)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           app.routes(),
		ReadHeaderTimeout: defaultServerTimeout,
		ReadTimeout:       defaultServerTimeout,
		WriteTimeout:      defaultServerTimeout,
		IdleTimeout:       30 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen and serve: %v", err)
	}
}

func loadConfig() (config, error) {
	dsn := strings.TrimSpace(os.Getenv("WORKLOAD_DB_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("GOOSE_DBSTRING_DOCKER"))
	}
	if dsn == "" {
		return config{}, errors.New("WORKLOAD_DB_DSN or GOOSE_DBSTRING_DOCKER is required")
	}

	return config{
		Port: getEnv("WORKLOAD_PORT", defaultWorkloadPort),
		DSN:  dsn,
	}, nil
}

func loadQueries() (queries, error) {
	load := func(name string) (string, error) {
		raw, err := sqlFS.ReadFile("sql/" + name)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(raw)), nil
	}

	recentV1, err := load("oltp_recent_orders_v1.sql")
	if err != nil {
		return queries{}, err
	}
	recentV2Degraded, err := load("oltp_recent_orders_v2.sql")
	if err != nil {
		return queries{}, err
	}
	recentV2Fast, err := load("oltp_recent_orders_v2_optimized.sql")
	if err != nil {
		return queries{}, err
	}
	inventory, err := load("oltp_inventory_availability.sql")
	if err != nil {
		return queries{}, err
	}
	updateStatus, err := load("oltp_update_order_status.sql")
	if err != nil {
		return queries{}, err
	}
	logBatch, err := load("log_insert_event_batch.sql")
	if err != nil {
		return queries{}, err
	}
	dailySalesV1, err := load("olap_daily_sales_v1.sql")
	if err != nil {
		return queries{}, err
	}
	dailySalesMV, err := load("olap_daily_sales_mv.sql")
	if err != nil {
		return queries{}, err
	}
	topCustomers, err := load("olap_top_customers.sql")
	if err != nil {
		return queries{}, err
	}

	return queries{
		RecentOrdersV1:         recentV1,
		RecentOrdersV2Degraded: recentV2Degraded,
		RecentOrdersV2Fast:     recentV2Fast,
		Inventory:              inventory,
		UpdateStatus:           updateStatus,
		LogBatchInsert:         logBatch,
		DailySalesV1:           dailySalesV1,
		DailySalesMV:           dailySalesMV,
		TopCustomers:           topCustomers,
	}, nil
}

func detectCapabilities(ctx context.Context, db *sql.DB) (capabilities, error) {
	var caps capabilities

	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = 'order_risk_flags'
		)
	`).Scan(&caps.HasRiskFlags); err != nil {
		return capabilities{}, err
	}

	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'orders'
			  AND column_name = 'special_instructions'
		)
	`).Scan(&caps.HasSpecialInstructions); err != nil {
		return capabilities{}, err
	}

	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_matviews
			WHERE schemaname = 'public'
			  AND matviewname = 'mv_daily_category_sales'
		)
	`).Scan(&caps.HasDailySalesMV); err != nil {
		return capabilities{}, err
	}

	return caps, nil
}

func fetchOrderCount(ctx context.Context, db *sql.DB) (int64, error) {
	var count int64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/api/oltp/orders/recent", a.handleRecentOrders)
	mux.HandleFunc("/api/oltp/inventory/availability", a.handleInventoryAvailability)
	mux.HandleFunc("/api/oltp/orders/status", a.handleAdvanceOrderStatus)
	mux.HandleFunc("/api/olap/sales/daily", a.handleDailySales)
	mux.HandleFunc("/api/olap/customers/top", a.handleTopCustomers)
	mux.HandleFunc("/api/logs/events", a.handleInsertEvents)
	return mux
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), a.requestTimeout)
	defer cancel()

	var one int
	if err := a.db.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil {
		writeError(w, http.StatusServiceUnavailable, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"orders":      a.orderCount,
		"schema_mode": a.caps,
	})
}

func (a *app) handleRecentOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}

	days := int64(getIntQuery(r, "days", defaultRecentDays))
	limit := int64(getIntQuery(r, "limit", defaultRecentLimit))
	offset := int64(getIntQuery(r, "offset", 0))

	query := a.queries.RecentOrdersV1
	if a.caps.HasRiskFlags && a.caps.HasSpecialInstructions {
		query = a.queries.RecentOrdersV2Degraded
		if a.caps.HasDailySalesMV {
			query = a.queries.RecentOrdersV2Fast
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), a.requestTimeout)
	defer cancel()

	rows, err := a.db.QueryContext(ctx, query, days, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	type recentOrder struct {
		ID               int64      `json:"id"`
		CreatedAt        time.Time  `json:"created_at"`
		UserID           int64      `json:"user_id"`
		FullName         string     `json:"full_name"`
		StatusName       string     `json:"status_name"`
		ItemsCount       int64      `json:"items_count"`
		OrderTotal       float64    `json:"order_total"`
		SampleProduct    string     `json:"sample_product_name"`
		RiskFlags        *string    `json:"risk_flags,omitempty"`
		LastEventAt      *time.Time `json:"last_event_at,omitempty"`
		SpecialNoteBytes *int64     `json:"note_length,omitempty"`
	}

	var result []recentOrder
	for rows.Next() {
		item := recentOrder{}
		if query != a.queries.RecentOrdersV1 {
			var riskFlags sql.NullString
			var lastEventAt sql.NullTime
			var noteLen sql.NullInt64
			if err := rows.Scan(
				&item.ID,
				&item.CreatedAt,
				&item.UserID,
				&item.FullName,
				&item.StatusName,
				&item.ItemsCount,
				&item.OrderTotal,
				&riskFlags,
				&lastEventAt,
				&noteLen,
				&item.SampleProduct,
			); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			if riskFlags.Valid {
				item.RiskFlags = &riskFlags.String
			}
			if lastEventAt.Valid {
				item.LastEventAt = &lastEventAt.Time
			}
			if noteLen.Valid {
				item.SpecialNoteBytes = &noteLen.Int64
			}
		} else {
			if err := rows.Scan(
				&item.ID,
				&item.CreatedAt,
				&item.UserID,
				&item.FullName,
				&item.StatusName,
				&item.ItemsCount,
				&item.OrderTotal,
				&item.SampleProduct,
			); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile": "oltp_read_recent_orders",
		"count":   len(result),
		"data":    result,
	})
}

func (a *app) handleInventoryAvailability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}

	search := r.URL.Query().Get("search")
	if strings.TrimSpace(search) == "" {
		search = defaultInventoryTerm
	} else if !strings.Contains(search, "%") {
		search = "%" + search + "%"
	}
	limit := int64(getIntQuery(r, "limit", defaultInventoryLimit))

	ctx, cancel := context.WithTimeout(r.Context(), a.requestTimeout)
	defer cancel()

	rows, err := a.db.QueryContext(ctx, a.queries.Inventory, search, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	type inventoryRow struct {
		ProductID           int64   `json:"product_id"`
		Name                string  `json:"name"`
		CategoryName        string  `json:"category_name"`
		CurrentPrice        float64 `json:"current_price"`
		TotalWarehouseStock int64   `json:"total_warehouse_stock"`
		AvgSupplierStock    float64 `json:"avg_supplier_stock"`
	}

	var result []inventoryRow
	for rows.Next() {
		item := inventoryRow{}
		if err := rows.Scan(
			&item.ProductID,
			&item.Name,
			&item.CategoryName,
			&item.CurrentPrice,
			&item.TotalWarehouseStock,
			&item.AvgSupplierStock,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile": "oltp_read_inventory",
		"count":   len(result),
		"data":    result,
	})
}

func (a *app) handleAdvanceOrderStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}

	orderID := int64(getIntQuery(r, "order_id", 0))
	if orderID <= 0 {
		orderID = a.nextOrderID()
	}

	ctx, cancel := context.WithTimeout(r.Context(), a.requestTimeout)
	defer cancel()

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer tx.Rollback()

	var newStatusID int64
	var createdAt time.Time
	if err := tx.QueryRowContext(ctx, a.queries.UpdateStatus, orderID).Scan(&orderID, &newStatusID, &createdAt); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO order_event_log (order_id, event_type, actor_type, payload)
		VALUES ($1, 'status_transition', 'workload_service', jsonb_build_object('new_status_id', $2::bigint))
	`, orderID, newStatusID); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile":       "oltp_write_status_update",
		"order_id":      orderID,
		"new_status_id": newStatusID,
		"created_at":    createdAt,
	})
}

func (a *app) handleDailySales(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}

	days := int64(getIntQuery(r, "days", 365))
	limit := int64(getIntQuery(r, "limit", 180))
	query := a.queries.DailySalesV1
	if a.caps.HasDailySalesMV {
		query = a.queries.DailySalesMV
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*a.requestTimeout)
	defer cancel()

	rows, err := a.db.QueryContext(ctx, query, days, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	type dailyRow struct {
		DayBucket    time.Time `json:"day_bucket"`
		CategoryName string    `json:"category_name"`
		OrdersCount  int64     `json:"orders_count"`
		ItemsSold    int64     `json:"items_sold"`
		Revenue      float64   `json:"revenue"`
	}

	var result []dailyRow
	for rows.Next() {
		item := dailyRow{}
		if err := rows.Scan(
			&item.DayBucket,
			&item.CategoryName,
			&item.OrdersCount,
			&item.ItemsSold,
			&item.Revenue,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile": "olap_daily_sales",
		"count":   len(result),
		"data":    result,
		"source":  ternary(a.caps.HasDailySalesMV, "materialized_view", "raw_query"),
	})
}

func (a *app) handleTopCustomers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}

	days := int64(getIntQuery(r, "days", 365))
	limit := int64(getIntQuery(r, "limit", defaultTopCustomers))

	ctx, cancel := context.WithTimeout(r.Context(), 2*a.requestTimeout)
	defer cancel()

	rows, err := a.db.QueryContext(ctx, a.queries.TopCustomers, days, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	defer rows.Close()

	type customerRow struct {
		UserID      int64     `json:"user_id"`
		FullName    string    `json:"full_name"`
		Revenue     float64   `json:"revenue"`
		OrdersCount int64     `json:"orders_count"`
		LastOrderAt time.Time `json:"last_order_at"`
		RevenueRank int64     `json:"revenue_rank"`
	}

	var result []customerRow
	for rows.Next() {
		item := customerRow{}
		if err := rows.Scan(
			&item.UserID,
			&item.FullName,
			&item.Revenue,
			&item.OrdersCount,
			&item.LastOrderAt,
			&item.RevenueRank,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		result = append(result, item)
	}

	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile": "olap_top_customers",
		"count":   len(result),
		"data":    result,
	})
}

func (a *app) handleInsertEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}

	batchSize := int64(getIntQuery(r, "batch_size", defaultLogBatchSize))
	offset := a.eventSeed.Add(batchSize)

	ctx, cancel := context.WithTimeout(r.Context(), a.requestTimeout)
	defer cancel()

	result, err := a.db.ExecContext(ctx, a.queries.LogBatchInsert, offset, a.orderCount, batchSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile":       "log_insert_events",
		"rows_affected": rowsAffected,
		"batch_size":    batchSize,
	})
}

func (a *app) nextOrderID() int64 {
	next := a.orderCursor.Add(1)
	return ((next - 1) % a.orderCount) + 1
}

func getIntQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func getEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func ternary[T any](condition bool, left, right T) T {
	if condition {
		return left
	}
	return right
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	log.Printf("workload request failed: status=%d err=%v", status, err)
	writeJSON(w, status, map[string]any{
		"error": err.Error(),
	})
}

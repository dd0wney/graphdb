package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"sync/atomic"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/licensing"
)

var (
	port          = flag.String("port", "", "HTTP server port (or set PORT)")
	databaseURL   = flag.String("database-url", "", "PostgreSQL connection string (or set DATABASE_URL)")
	dataDir       = flag.String("data", "./data/licenses", "License data directory (fallback if no DATABASE_URL)")
	stripeKey     = flag.String("stripe-key", "", "Stripe secret key (or set STRIPE_SECRET_KEY)")
	webhookSecret = flag.String("webhook-secret", "", "Stripe webhook secret (or set STRIPE_WEBHOOK_SECRET)")
)

type Server struct {
	store           licensing.LicenseStore
	stripeKey       string
	webhookSecret   string
	logger          *slog.Logger
	startTime       time.Time
	licensesValidated atomic.Int64
	licensesFailed    atomic.Int64
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	flag.Parse()

	// Structured logging (Railway best practice)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Get config from env vars (Railway best practice)
	if *port == "" {
		*port = getEnv("PORT", "8080")
	}
	if *databaseURL == "" {
		*databaseURL = os.Getenv("DATABASE_URL")
	}
	if *stripeKey == "" {
		*stripeKey = os.Getenv("STRIPE_SECRET_KEY")
	}
	if *webhookSecret == "" {
		*webhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	}

	// Initialize store (PostgreSQL preferred, fallback to JSON)
	var store licensing.LicenseStore
	var err error

	if *databaseURL != "" {
		logger.Info("initializing PostgreSQL store")
		store, err = licensing.NewPGStore(*databaseURL)
		if err != nil {
			logger.Error("failed to initialize PostgreSQL store", "error", err)
			logger.Info("falling back to JSON file store")
			store, err = licensing.NewStore(*dataDir)
			if err != nil {
				logger.Error("failed to initialize store", "error", err)
				os.Exit(1)
			}
		}
	} else {
		logger.Info("initializing JSON file store", "data_dir", *dataDir)
		store, err = licensing.NewStore(*dataDir)
		if err != nil {
			logger.Error("failed to initialize store", "error", err)
			os.Exit(1)
		}
	}
	defer store.Close()

	if *stripeKey == "" {
		logger.Warn("no Stripe secret key provided - Stripe integration disabled")
	}

	server := &Server{
		store:         store,
		stripeKey:     *stripeKey,
		webhookSecret: *webhookSecret,
		logger:        logger,
		startTime:     time.Now(),
	}

	// Setup routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/metrics", server.handleMetrics)
	mux.HandleFunc("/validate", server.handleValidate)
	mux.HandleFunc("/stripe/webhook", server.handleStripeWebhook)
	mux.HandleFunc("/licenses", server.handleListLicenses)
	mux.HandleFunc("/licenses/create", server.handleCreateLicense)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         ":" + *port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		logger.Info("license server starting",
			"port", *port,
			"database", *databaseURL != "",
			"stripe", *stripeKey != "",
		)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal (Railway best practice)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

	// Graceful shutdown with 30s timeout (Railway best practice)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("server forced to shutdown", "error", err)
	}

	logger.Info("server exited")
}

// handleHealth returns server health status and verifies DB connection
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Check database connection (Railway best practice)
	if err := s.store.Ping(); err != nil {
		s.logger.Error("health check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "unhealthy",
			"database": "disconnected",
			"error":    err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "healthy",
		"database": "connected",
		"uptime":   time.Since(s.startTime).Seconds(),
	})
}

// handleMetrics returns custom metrics
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"licenses_validated": s.licensesValidated.Load(),
		"licenses_failed":    s.licensesFailed.Load(),
		"uptime_seconds":     time.Since(s.startTime).Seconds(),
	})
}

// handleValidate validates a license key
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		LicenseKey string `json:"license_key"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Basic format validation
	if !licensing.ValidateLicenseKey(req.LicenseKey) {
		s.licensesFailed.Add(1)
		s.logger.Info("license validation failed", "reason", "invalid_format", "key", req.LicenseKey)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":  false,
			"reason": "invalid_format",
		})
		return
	}

	// Look up license
	license, err := s.store.GetLicenseByKey(req.LicenseKey)
	if err != nil {
		s.licensesFailed.Add(1)
		s.logger.Info("license validation failed", "reason", "not_found", "key", req.LicenseKey)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":  false,
			"reason": "not_found",
		})
		return
	}

	// Check if active
	if !license.IsActive() {
		s.licensesFailed.Add(1)
		s.logger.Info("license validation failed", "reason", "inactive", "key", req.LicenseKey, "status", license.Status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":  false,
			"reason": "inactive",
			"status": license.Status,
		})
		return
	}

	// Valid license
	s.licensesValidated.Add(1)
	s.logger.Info("license validated", "key", req.LicenseKey, "type", license.Type, "email", license.Email)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"valid":      true,
		"type":       license.Type,
		"email":      license.Email,
		"created_at": license.CreatedAt.Unix(),
		"expires_at": license.ExpiresAt,
	})
}

// handleStripeWebhook processes Stripe webhook events
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("error reading webhook body", "error", err)
		http.Error(w, "Error reading request", http.StatusBadRequest)
		return
	}

	// For now, we'll parse the JSON directly
	// In production, you should verify the Stripe signature
	var event struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		s.logger.Error("error parsing webhook", "error", err)
		http.Error(w, "Error parsing webhook", http.StatusBadRequest)
		return
	}

	s.logger.Info("received Stripe webhook", "type", event.Type)

	switch event.Type {
	case "checkout.session.completed":
		s.handleCheckoutCompleted(event.Data)
	case "customer.subscription.updated":
		s.handleSubscriptionUpdated(event.Data)
	case "customer.subscription.deleted":
		s.handleSubscriptionDeleted(event.Data)
	default:
		s.logger.Info("unhandled event type", "type", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

// handleCheckoutCompleted creates a new license when payment succeeds
func (s *Server) handleCheckoutCompleted(data json.RawMessage) {
	var session struct {
		Object struct {
			CustomerEmail string            `json:"customer_email"`
			Customer      string            `json:"customer"`
			Subscription  string            `json:"subscription"`
			Metadata      map[string]string `json:"metadata"`
		} `json:"object"`
	}

	if err := json.Unmarshal(data, &session); err != nil {
		s.logger.Error("error parsing checkout session", "error", err)
		return
	}

	// Determine license type from metadata or line items
	licenseType := licensing.LicenseTypeProfessional
	if session.Object.Metadata["type"] == "enterprise" {
		licenseType = licensing.LicenseTypeEnterprise
	}

	// Generate license key
	licenseKey, err := licensing.GenerateLicenseKey(licenseType, session.Object.CustomerEmail)
	if err != nil {
		s.logger.Error("error generating license key", "error", err)
		return
	}

	// Create license
	license := &licensing.License{
		ID:             licensing.GenerateLicenseID(),
		Key:            licenseKey,
		Type:           licenseType,
		Email:          session.Object.CustomerEmail,
		CustomerID:     session.Object.Customer,
		SubscriptionID: session.Object.Subscription,
		Status:         "active",
		CreatedAt:      time.Now(),
		Metadata:       session.Object.Metadata,
	}

	if err := s.store.CreateLicense(license); err != nil {
		s.logger.Error("error creating license", "error", err)
		return
	}

	s.logger.Info("license created",
		"key", license.Key,
		"email", license.Email,
		"type", license.Type,
	)

	// TODO: Send license key via email
}

// handleSubscriptionUpdated updates license status
func (s *Server) handleSubscriptionUpdated(data json.RawMessage) {
	var subscription struct {
		Object struct {
			ID       string `json:"id"`
			Customer string `json:"customer"`
			Status   string `json:"status"`
		} `json:"object"`
	}

	if err := json.Unmarshal(data, &subscription); err != nil {
		s.logger.Error("error parsing subscription", "error", err)
		return
	}

	// Find license by customer ID
	license, err := s.store.GetLicenseByCustomer(subscription.Object.Customer)
	if err != nil {
		s.logger.Warn("license not found for customer", "customer_id", subscription.Object.Customer)
		return
	}

	// Update status
	license.Status = subscription.Object.Status
	if err := s.store.UpdateLicense(license); err != nil {
		s.logger.Error("error updating license", "error", err)
		return
	}

	s.logger.Info("license updated",
		"key", license.Key,
		"status", license.Status,
	)
}

// handleSubscriptionDeleted marks license as cancelled
func (s *Server) handleSubscriptionDeleted(data json.RawMessage) {
	var subscription struct {
		Object struct {
			ID       string `json:"id"`
			Customer string `json:"customer"`
		} `json:"object"`
	}

	if err := json.Unmarshal(data, &subscription); err != nil {
		s.logger.Error("error parsing subscription", "error", err)
		return
	}

	// Find license by customer ID
	license, err := s.store.GetLicenseByCustomer(subscription.Object.Customer)
	if err != nil {
		s.logger.Warn("license not found for customer", "customer_id", subscription.Object.Customer)
		return
	}

	// Mark as cancelled
	license.Status = "cancelled"
	if err := s.store.UpdateLicense(license); err != nil {
		s.logger.Error("error updating license", "error", err)
		return
	}

	s.logger.Info("license cancelled", "key", license.Key)
}

// handleListLicenses lists all licenses (admin endpoint, should be protected)
func (s *Server) handleListLicenses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// TODO: Add authentication
	licenses := s.store.ListLicenses()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"licenses": licenses,
		"count":    len(licenses),
	})
}

// handleCreateLicense creates a license manually (for testing)
func (s *Server) handleCreateLicense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email string                `json:"email"`
		Type  licensing.LicenseType `json:"type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Generate license key
	licenseKey, err := licensing.GenerateLicenseKey(req.Type, req.Email)
	if err != nil {
		http.Error(w, "Error generating license key", http.StatusInternalServerError)
		return
	}

	// Create license
	license := &licensing.License{
		ID:        licensing.GenerateLicenseID(),
		Key:       licenseKey,
		Type:      req.Type,
		Email:     req.Email,
		Status:    "active",
		CreatedAt: time.Now(),
	}

	if err := s.store.CreateLicense(license); err != nil {
		http.Error(w, "Error creating license", http.StatusInternalServerError)
		return
	}

	s.logger.Info("test license created", "key", license.Key, "email", license.Email)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(license)
}

package admin

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// RegisterHandlers registers admin HTTP handlers
func (um *UpgradeManager) RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/admin/upgrade/status", um.handleUpgradeStatus)
	mux.HandleFunc("/admin/upgrade/promote", um.handlePromote)
	mux.HandleFunc("/admin/upgrade/stepdown", um.handleStepDown)
}

func (um *UpgradeManager) handleUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := um.GetUpgradeStatus()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (um *UpgradeManager) handlePromote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PromoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Use defaults if no body provided
		req.WaitForSync = true
		req.Timeout = 60 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), req.Timeout)
	defer cancel()

	response, err := um.PromoteToPrimary(ctx, req.WaitForSync, req.Timeout)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else if response.Success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}

	if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
		log.Printf("Failed to encode promote response: %v", encErr)
	}
}

func (um *UpgradeManager) handleStepDown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req StepDownRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.NewPrimaryID == "" {
		http.Error(w, "new_primary_id is required", http.StatusBadRequest)
		return
	}

	if req.Timeout == 0 {
		req.Timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), req.Timeout)
	defer cancel()

	response, err := um.StepDownToReplica(ctx, req.NewPrimaryID)

	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	} else if response.Success {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusBadRequest)
	}

	if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
		log.Printf("Failed to encode stepdown response: %v", encErr)
	}
}

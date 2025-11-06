package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/SahithiKokkula/backend-2-cent-ventures/engine"
)

// RecoveryHandler handles recovery-related HTTP requests
type RecoveryHandler struct {
	recoveryManager *engine.RecoveryManager
	orderbook       *engine.OrderBook
}

// NewRecoveryHandler creates a new recovery handler
func NewRecoveryHandler(recoveryManager *engine.RecoveryManager, orderbook *engine.OrderBook) *RecoveryHandler {
	return &RecoveryHandler{
		recoveryManager: recoveryManager,
		orderbook:       orderbook,
	}
}

// RecoveryResponse represents the response for recovery operations
type RecoveryResponse struct {
	Success bool                   `json:"success"`
	Message string                 `json:"message,omitempty"`
	Report  *engine.RecoveryReport `json:"report,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// HandleTriggerRecovery handles POST /admin/recovery/trigger
func (h *RecoveryHandler) HandleTriggerRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.respondJSON(w, http.StatusMethodNotAllowed, RecoveryResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	dryRun := r.URL.Query().Get("dry_run") == "true"
	verbose := r.URL.Query().Get("verbose") == "true"

	if dryRun {
		log.Println("🔍 DRY-RUN recovery triggered via API (no changes will be applied)...")
	} else {
		log.Println("🔄 Manual recovery triggered via API...")
	}

	opts := engine.RecoveryOptions{
		DryRun:      dryRun,
		Verbose:     verbose,
		FailOnError: false,
	}

	report, err := h.recoveryManager.RecoverOrderbookWithOptions(r.Context(), h.orderbook, opts)
	if err != nil {
		log.Printf("Recovery failed: %v", err)
		h.respondJSON(w, http.StatusInternalServerError, RecoveryResponse{
			Success: false,
			Error:   fmt.Sprintf("Recovery failed: %v", err),
		})
		return
	}

	if !report.ValidationPassed {
		message := fmt.Sprintf("Recovery completed with %d validation errors", len(report.ValidationErrors))
		if dryRun {
			message = "DRY-RUN: " + message
		}
		h.respondJSON(w, http.StatusOK, RecoveryResponse{
			Success: false,
			Message: message,
			Report:  report,
		})
		return
	}

	message := fmt.Sprintf("Recovery completed successfully: %d orders restored in %v",
		report.OrdersRestored, report.Duration)
	if dryRun {
		message = "DRY-RUN: " + message + " (no changes applied)"
	}

	h.respondJSON(w, http.StatusOK, RecoveryResponse{
		Success: true,
		Message: message,
		Report:  report,
	})
}

func (h *RecoveryHandler) HandleRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.respondJSON(w, http.StatusMethodNotAllowed, RecoveryResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	err := h.recoveryManager.QuickRecoveryCheck(r.Context())
	if err != nil {
		h.respondJSON(w, http.StatusServiceUnavailable, RecoveryResponse{
			Success: false,
			Error:   fmt.Sprintf("Recovery check failed: %v", err),
		})
		return
	}

	h.respondJSON(w, http.StatusOK, RecoveryResponse{
		Success: true,
		Message: "Recovery system healthy - database accessible, snapshots available",
	})
}

// respondJSON writes JSON response
func (h *RecoveryHandler) respondJSON(w http.ResponseWriter, statusCode int, response RecoveryResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(response)
}

// RegisterRoutes registers recovery routes with a mux
func (h *RecoveryHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/recovery/trigger", h.HandleTriggerRecovery)
	mux.HandleFunc("/admin/recovery/status", h.HandleRecoveryStatus)

	log.Println("🔄 Recovery routes registered:")
	log.Println("  POST   /admin/recovery/trigger              - Trigger manual recovery")
	log.Println("  POST   /admin/recovery/trigger?dry_run=true - Simulate recovery (no changes)")
	log.Println("  POST   /admin/recovery/trigger?verbose=true - Include detailed diffs")
	log.Println("  GET    /admin/recovery/status               - Check recovery system health")
}

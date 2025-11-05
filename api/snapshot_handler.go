package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/yourusername/trading-engine/engine"
)

// SnapshotHandler handles snapshot-related HTTP requests
type SnapshotHandler struct {
	snapshotManager *engine.SnapshotManager
}

// NewSnapshotHandler creates a new snapshot handler
func NewSnapshotHandler(snapshotManager *engine.SnapshotManager) *SnapshotHandler {
	return &SnapshotHandler{
		snapshotManager: snapshotManager,
	}
}

// TriggerSnapshotRequest represents the request body for triggering a snapshot
type TriggerSnapshotRequest struct {
	TriggeredBy string `json:"triggered_by"`
}

// SnapshotResponse represents the response for snapshot operations
type SnapshotResponse struct {
	Success    bool                      `json:"success"`
	Message    string                    `json:"message,omitempty"`
	Snapshot   *engine.OrderbookSnapshot `json:"snapshot,omitempty"`
	Mismatches []engine.SnapshotMismatch `json:"mismatches,omitempty"`
	Error      string                    `json:"error,omitempty"`
}

func (h *SnapshotHandler) HandleTriggerSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.respondJSON(w, http.StatusMethodNotAllowed, SnapshotResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req TriggerSnapshotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.TriggeredBy = "admin_api"
	}
	defer r.Body.Close()

	err := h.snapshotManager.TakeSnapshot(engine.SnapshotTypeOnDemand, req.TriggeredBy)
	if err != nil {
		log.Printf("Failed to take on-demand snapshot: %v", err)
		h.respondJSON(w, http.StatusInternalServerError, SnapshotResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to take snapshot: %v", err),
		})
		return
	}

	snapshot, err := h.snapshotManager.GetLatestSnapshot(r.Context())
	if err != nil {
		log.Printf("Failed to retrieve snapshot after creation: %v", err)
		h.respondJSON(w, http.StatusOK, SnapshotResponse{
			Success: true,
			Message: "Snapshot created successfully (unable to retrieve details)",
		})
		return
	}

	h.respondJSON(w, http.StatusCreated, SnapshotResponse{
		Success:  true,
		Message:  fmt.Sprintf("Snapshot created successfully (ID: %d)", snapshot.SnapshotID),
		Snapshot: snapshot,
	})
}

func (h *SnapshotHandler) HandleGetLatestSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.respondJSON(w, http.StatusMethodNotAllowed, SnapshotResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	snapshot, err := h.snapshotManager.GetLatestSnapshot(r.Context())
	if err != nil {
		h.respondJSON(w, http.StatusNotFound, SnapshotResponse{
			Success: false,
			Error:   fmt.Sprintf("No snapshot found: %v", err),
		})
		return
	}

	h.respondJSON(w, http.StatusOK, SnapshotResponse{
		Success:  true,
		Snapshot: snapshot,
	})
}

// HandleGetSnapshot handles GET /snapshots/history - returns list of recent snapshots
func (h *SnapshotHandler) HandleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.respondJSON(w, http.StatusMethodNotAllowed, SnapshotResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	// Parse query parameters
	instrument := r.URL.Query().Get("instrument")
	if instrument == "" {
		instrument = "BTC-USD" // Default instrument
	}

	// Parse limit (default 10)
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			if parsedLimit > 100 {
				limit = 100 // Cap at 100
			} else {
				limit = parsedLimit
			}
		}
	}

	// For now, return the latest snapshot info in an array
	// In a full implementation, this would query the database for historical snapshots
	snapshot, err := h.snapshotManager.GetLatestSnapshot(r.Context())
	if err != nil {
		response := map[string]interface{}{
			"success":    true,
			"instrument": instrument,
			"snapshots":  []interface{}{},
			"count":      0,
			"message":    "No snapshots available",
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Return snapshot in array format for history endpoint
	response := map[string]interface{}{
		"success":    true,
		"instrument": instrument,
		"snapshots": []map[string]interface{}{
			{
				"snapshot_id":   snapshot.SnapshotID,
				"instrument":    snapshot.Instrument,
				"timestamp":     snapshot.SnapshotTimestamp.Format("2006-01-02T15:04:05Z07:00"),
				"bid_levels":    len(snapshot.BidLevels),
				"ask_levels":    len(snapshot.AskLevels),
				"snapshot_type": snapshot.SnapshotType,
			},
		},
		"count": 1,
		"limit": limit,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// HandleRestoreSnapshot handles POST /admin/snapshot/{id}/restore
func (h *SnapshotHandler) HandleRestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.respondJSON(w, http.StatusMethodNotAllowed, SnapshotResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	snapshotIDStr := r.URL.Query().Get("id")
	if snapshotIDStr == "" {
		h.respondJSON(w, http.StatusBadRequest, SnapshotResponse{
			Success: false,
			Error:   "Missing snapshot ID",
		})
		return
	}

	snapshotID, err := strconv.ParseInt(snapshotIDStr, 10, 64)
	if err != nil {
		h.respondJSON(w, http.StatusBadRequest, SnapshotResponse{
			Success: false,
			Error:   "Invalid snapshot ID",
		})
		return
	}

	err = h.snapshotManager.RestoreFromSnapshot(r.Context(), snapshotID)
	if err != nil {
		h.respondJSON(w, http.StatusInternalServerError, SnapshotResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to restore snapshot: %v", err),
		})
		return
	}

	h.respondJSON(w, http.StatusOK, SnapshotResponse{
		Success: true,
		Message: fmt.Sprintf("Orderbook restored from snapshot %d", snapshotID),
	})
}

// HandleValidateSnapshot handles GET /admin/snapshot/validate
func (h *SnapshotHandler) HandleValidateSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.respondJSON(w, http.StatusMethodNotAllowed, SnapshotResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	mismatches, err := h.snapshotManager.ValidateLatestSnapshot(r.Context())
	if err != nil {
		h.respondJSON(w, http.StatusInternalServerError, SnapshotResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to validate snapshot: %v", err),
		})
		return
	}

	if len(mismatches) == 0 {
		h.respondJSON(w, http.StatusOK, SnapshotResponse{
			Success: true,
			Message: "Snapshot matches live orderbook perfectly",
		})
	} else {
		h.respondJSON(w, http.StatusOK, SnapshotResponse{
			Success:    false,
			Message:    fmt.Sprintf("Found %d mismatches between snapshot and live orderbook", len(mismatches)),
			Mismatches: mismatches,
		})
	}
}

// respondJSON writes JSON response
func (h *SnapshotHandler) respondJSON(w http.ResponseWriter, statusCode int, response SnapshotResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}

// RegisterRoutes registers snapshot routes with a mux
func (h *SnapshotHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/snapshot", h.HandleTriggerSnapshot)
	mux.HandleFunc("/admin/snapshot/latest", h.HandleGetLatestSnapshot)
	mux.HandleFunc("/admin/snapshot/get", h.HandleGetSnapshot)
	mux.HandleFunc("/admin/snapshot/restore", h.HandleRestoreSnapshot)
	mux.HandleFunc("/admin/snapshot/validate", h.HandleValidateSnapshot)

	log.Println("📸 Snapshot routes registered:")
	log.Println("  POST   /admin/snapshot          - Trigger on-demand snapshot")
	log.Println("  GET    /admin/snapshot/latest   - Get latest snapshot")
	log.Println("  GET    /admin/snapshot/get?id=X - Get specific snapshot")
	log.Println("  POST   /admin/snapshot/restore?id=X - Restore from snapshot")
	log.Println("  GET    /admin/snapshot/validate - Validate latest snapshot vs live orderbook")
}

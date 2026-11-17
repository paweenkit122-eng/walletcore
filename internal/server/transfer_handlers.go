package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"walletcore/internal/wallet"
)

// --- POST /api/v1/transfers ----------------------------------------------
// Move money between two accounts. Retry-safety key comes from the
// Idempotency-Key header. Calls wallet.Store.Transfer.

type transferRequest struct {
	FromAccountID int64 `json:"from_account_id"`
	ToAccountID   int64 `json:"to_account_id"`
	Amount        int64 `json:"amount"` // satang
}

func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key header is required"})
		return
	}

	var req transferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	result, err := s.wallet.Transfer(r.Context(), req.FromAccountID, req.ToAccountID, req.Amount, idempotencyKey)
	if err != nil {
		switch {
		case errors.Is(err, wallet.ErrInvalidAmount), errors.Is(err, wallet.ErrSameAccount):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, wallet.ErrInsufficientFunds):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		case errors.Is(err, wallet.ErrAccountNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"transaction_id":  result.TransactionID,
		"from_account_id": result.FromAccountID,
		"to_account_id":   result.ToAccountID,
		"from_balance":    result.FromBalance,
		"to_balance":      result.ToBalance,
	})
}

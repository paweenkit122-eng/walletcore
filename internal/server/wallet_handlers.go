package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"walletcore/internal/wallet"
)

// --- POST /api/v1/users --------------------------------------------------
// Dev helper: create a user + their THB wallet so we have something to top up.
// (Real registration/auth comes in Phase 1.)

type createUserRequest struct {
	Email string `json:"email"`
}

type createUserResponse struct {
	UserID    int64 `json:"user_id"`
	AccountID int64 `json:"account_id"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if req.Email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email is required"})
		return
	}

	userID, accountID, err := s.wallet.CreateUserWithWallet(r.Context(), req.Email)
	if err != nil {
		// TODO(hardening): don't echo raw DB errors to clients in production.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, createUserResponse{UserID: userID, AccountID: accountID})
}

// --- POST /api/v1/topups -------------------------------------------------
// Adds money to a wallet. The retry-safety key comes from the Idempotency-Key
// header. This handler is DONE — it calls wallet.Store.TopUp, which is the
// function you implement. Once TopUp works, this endpoint works.

type topUpRequest struct {
	AccountID int64 `json:"account_id"`
	Amount    int64 `json:"amount"` // satang
}

func (s *Server) handleTopUp(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key header is required"})
		return
	}

	var req topUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	result, err := s.wallet.TopUp(r.Context(), req.AccountID, req.Amount, idempotencyKey)
	if err != nil {
		if errors.Is(err, wallet.ErrInvalidAmount) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// Don't leak internal/DB error details to clients.
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"transaction_id": result.TransactionID,
		"account_id":     result.AccountID,
		"new_balance":    result.NewBalance,
	})
}

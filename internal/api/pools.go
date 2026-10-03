package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"investgo/internal/core/instrument"
	"investgo/internal/core/pool"
)

const (
	defaultPoolPageSize = 20
	maxPoolPageSize     = 100
)

type poolDTO struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Market      string                `json:"market"`
	AssetClass  instrument.AssetClass `json:"assetClass"`
	Type        pool.Type             `json:"type"`
	DataVersion string                `json:"dataVersion"`
	CreatedAt   time.Time             `json:"createdAt"`
	UpdatedAt   time.Time             `json:"updatedAt"`
}

type instrumentDTO struct {
	ID            string                      `json:"id"`
	AssetClass    instrument.AssetClass       `json:"assetClass"`
	Symbol        string                      `json:"symbol"`
	Name          string                      `json:"name"`
	DefaultName   string                      `json:"defaultName,omitempty"`
	HasCustomName bool                        `json:"hasCustomName,omitzero"`
	Market        string                      `json:"market"`
	Exchange      string                      `json:"exchange"`
	BaseAsset     string                      `json:"baseAsset,omitempty"`
	QuoteCurrency string                      `json:"quoteCurrency"`
	Status        instrument.InstrumentStatus `json:"status"`
	UpdatedAt     time.Time                   `json:"updatedAt"`
}

type poolMemberDTO struct {
	PoolID     string            `json:"poolId"`
	Instrument instrumentDTO     `json:"instrument"`
	Source     pool.MemberSource `json:"source"`
	Status     pool.MemberStatus `json:"status"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

type poolPageResponse struct {
	Items    []poolDTO `json:"items"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
	Total    int       `json:"total"`
	HasMore  bool      `json:"hasMore"`
}

type poolMemberPageResponse struct {
	Items    []poolMemberDTO `json:"items"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int             `json:"total"`
	HasMore  bool            `json:"hasMore"`
}

type addPoolMemberRequest struct {
	AssetClass    instrument.AssetClass `json:"assetClass"`
	Symbol        string                `json:"symbol"`
	Name          string                `json:"name"`
	Market        string                `json:"market"`
	Exchange      string                `json:"exchange"`
	BaseAsset     string                `json:"baseAsset"`
	QuoteCurrency string                `json:"quoteCurrency"`
}

type updatePoolMemberRequest struct {
	Symbol    string `json:"symbol"`
	Name      string `json:"name"`
	ResetName bool   `json:"resetName,omitzero"`
}

func (h *Handler) handlePools(writer http.ResponseWriter, request *http.Request) {
	if !h.requirePoolService(writer, request) {
		return
	}
	page, pageSize, err := parsePoolPagination(request)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, err)
		return
	}
	values, err := h.pools.ListPools(request.Context())
	if err != nil {
		h.writePoolError(writer, request, err)
		return
	}
	start, end := paginatePoolValues(len(values), page, pageSize)
	items := make([]poolDTO, 0, end-start)
	for _, value := range values[start:end] {
		items = append(items, newPoolDTO(value))
	}
	writeJSON(writer, http.StatusOK, poolPageResponse{
		Items: items, Page: page, PageSize: pageSize, Total: len(values), HasMore: end < len(values),
	})
}

func (h *Handler) handlePoolMembers(writer http.ResponseWriter, request *http.Request) {
	if !h.requirePoolService(writer, request) {
		return
	}
	page, pageSize, err := parsePoolPagination(request)
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, err)
		return
	}
	values, err := h.pools.ListMembers(request.Context(), request.PathValue("id"))
	if err != nil {
		h.writePoolError(writer, request, err)
		return
	}
	status := pool.MemberStatus(strings.ToLower(strings.TrimSpace(request.URL.Query().Get("status"))))
	if status != "" && status != pool.MemberStatusActive && status != pool.MemberStatusExcluded {
		writeError(
			writer,
			request,
			http.StatusBadRequest,
			&apiError{message: "Pool member status must be active or excluded"},
		)
		return
	}
	filtered := make([]pool.Member, 0, len(values))
	for _, value := range values {
		if status == "" || value.Status == status {
			filtered = append(filtered, value)
		}
	}
	start, end := paginatePoolValues(len(filtered), page, pageSize)
	items := make([]poolMemberDTO, 0, end-start)
	for _, value := range filtered[start:end] {
		items = append(items, newPoolMemberDTO(value))
	}
	writeJSON(writer, http.StatusOK, poolMemberPageResponse{
		Items: items, Page: page, PageSize: pageSize, Total: len(filtered), HasMore: end < len(filtered),
	})
}

func (h *Handler) handleAddPoolMember(writer http.ResponseWriter, request *http.Request) {
	if !h.requirePoolService(writer, request) {
		return
	}
	var payload addPoolMemberRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(writer, request, http.StatusBadRequest, err)
		return
	}
	candidate, err := instrument.Normalize(instrument.Instrument{
		AssetClass:    payload.AssetClass,
		Symbol:        payload.Symbol,
		Name:          payload.Name,
		Market:        payload.Market,
		Exchange:      payload.Exchange,
		BaseAsset:     payload.BaseAsset,
		QuoteCurrency: payload.QuoteCurrency,
	})
	if err != nil {
		writeError(writer, request, http.StatusBadRequest, err)
		return
	}
	poolID := request.PathValue("id")
	alreadyPresent := h.poolContainsIdentity(request, poolID, candidate.Identity())
	member, err := h.pools.AddMember(request.Context(), poolID, candidate)
	if err != nil {
		h.writePoolError(writer, request, err)
		return
	}
	h.invalidatePool(poolID)
	status := http.StatusCreated
	if alreadyPresent {
		status = http.StatusOK
	}
	writeJSON(writer, status, newPoolMemberDTO(member))
}

func (h *Handler) handleUpdatePoolMember(writer http.ResponseWriter, request *http.Request) {
	if !h.requirePoolService(writer, request) {
		return
	}
	var payload updatePoolMemberRequest
	if err := decodeJSON(request, &payload); err != nil {
		writeError(writer, request, http.StatusBadRequest, err)
		return
	}
	poolID := request.PathValue("id")
	member, err := h.pools.UpdateMember(
		request.Context(),
		poolID,
		request.PathValue("instrumentId"),
		pool.UpdateMemberInput{
			Symbol:    payload.Symbol,
			Name:      payload.Name,
			ResetName: payload.ResetName,
		},
	)
	if err != nil {
		h.writePoolError(writer, request, err)
		return
	}
	h.invalidateHotLists()
	if err := h.syncInstrumentDisplay(member.Instrument); err != nil {
		writeError(writer, request, http.StatusInternalServerError, err)
		return
	}
	writeJSON(writer, http.StatusOK, newPoolMemberDTO(member))
}

func (h *Handler) handleDeletePoolMember(writer http.ResponseWriter, request *http.Request) {
	if !h.requirePoolService(writer, request) {
		return
	}
	poolID := request.PathValue("id")
	instrumentID := request.PathValue("instrumentId")
	members, err := h.pools.ListMembers(request.Context(), poolID)
	if err != nil {
		h.writePoolError(writer, request, err)
		return
	}
	var target *pool.Member
	for index := range members {
		if members[index].Instrument.ID == instrumentID {
			target = &members[index]
			break
		}
	}
	if target == nil {
		h.writePoolError(writer, request, poolMemberNotFound(poolID, instrumentID))
		return
	}
	if target.Status == pool.MemberStatusExcluded {
		writeError(writer, request, http.StatusConflict, &apiError{message: "Pool member is already excluded"})
		return
	}
	if target.Source == pool.MemberSourceUser {
		if err := h.pools.DeleteUserMember(request.Context(), poolID, instrumentID); err != nil {
			h.writePoolError(writer, request, err)
			return
		}
		h.invalidatePool(poolID)
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	member, err := h.pools.ExcludeMember(request.Context(), poolID, instrumentID)
	if err != nil {
		h.writePoolError(writer, request, err)
		return
	}
	h.invalidatePool(poolID)
	writeJSON(writer, http.StatusOK, newPoolMemberDTO(member))
}

func (h *Handler) handleRestorePoolMember(writer http.ResponseWriter, request *http.Request) {
	if !h.requirePoolService(writer, request) {
		return
	}
	poolID := request.PathValue("id")
	member, err := h.pools.RestoreMember(request.Context(), poolID, request.PathValue("instrumentId"))
	if err != nil {
		h.writePoolError(writer, request, err)
		return
	}
	h.invalidatePool(poolID)
	writeJSON(writer, http.StatusOK, newPoolMemberDTO(member))
}

func (h *Handler) poolContainsIdentity(request *http.Request, poolID string, identity instrument.Identity) bool {
	members, err := h.pools.ListMembers(request.Context(), poolID)
	if err != nil {
		return false
	}
	for _, member := range members {
		if member.Status == pool.MemberStatusActive && member.Instrument.Identity() == identity {
			return true
		}
	}
	return false
}

func (h *Handler) requirePoolService(writer http.ResponseWriter, request *http.Request) bool {
	if h.pools != nil {
		return true
	}
	writeError(writer, request, http.StatusServiceUnavailable, &apiError{message: "Pool service is unavailable"})
	return false
}

func (h *Handler) invalidatePool(poolID string) {
	if h.hot != nil {
		h.hot.InvalidatePool(poolID)
	}
}

func (h *Handler) invalidateHotLists() {
	for _, poolID := range []string{
		pool.PoolIDCNA,
		pool.PoolIDCNETF,
		pool.PoolIDHK,
		pool.PoolIDHKETF,
		pool.PoolIDUSSP500,
		pool.PoolIDUSNasdaq,
		pool.PoolIDUSDow,
		pool.PoolIDUSETF,
	} {
		h.invalidatePool(poolID)
	}
}

func (h *Handler) syncInstrumentDisplay(value instrument.Instrument) error {
	if h.store == nil {
		return nil
	}
	if err := h.store.ApplyInstrumentDisplay(value); err != nil {
		if h.logs != nil {
			h.logs.Warn("backend", "pools", "apply instrument display: "+err.Error())
		}
		return fmt.Errorf("failed to sync instrument display name: %w", err)
	}
	return nil
}

func (h *Handler) writePoolError(writer http.ResponseWriter, request *http.Request, err error) {
	writeClassifiedError(writer, request, err)
}

func parsePoolPagination(request *http.Request) (int, int, error) {
	page, err := parseOptionalIntQuery(request, "page")
	if err != nil {
		return 0, 0, err
	}
	pageSize, err := parseOptionalIntQuery(request, "pageSize")
	if err != nil {
		return 0, 0, err
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultPoolPageSize
	}
	if pageSize > maxPoolPageSize {
		return 0, 0, &apiError{message: "Pool pageSize must not exceed 100"}
	}
	return page, pageSize, nil
}

func paginatePoolValues(total, page, pageSize int) (int, int) {
	start := (page - 1) * pageSize
	if start >= total {
		return total, total
	}
	return start, min(start+pageSize, total)
}

func newPoolDTO(value pool.Pool) poolDTO {
	return poolDTO{
		ID: value.ID, Name: value.Name, Market: value.Market, AssetClass: value.AssetClass,
		Type: value.Type, DataVersion: value.DataVersion, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}
}

func newPoolMemberDTO(value pool.Member) poolMemberDTO {
	return poolMemberDTO{
		PoolID: value.PoolID,
		Instrument: instrumentDTO{
			ID:            value.Instrument.ID,
			AssetClass:    value.Instrument.AssetClass,
			Symbol:        value.Instrument.Symbol,
			Name:          value.Instrument.Display(),
			DefaultName:   value.Instrument.Name,
			HasCustomName: value.Instrument.HasCustomName(),
			Market:        value.Instrument.Market,
			Exchange:      value.Instrument.Exchange,
			BaseAsset:     value.Instrument.BaseAsset,
			QuoteCurrency: value.Instrument.QuoteCurrency,
			Status:        value.Instrument.Status,
			UpdatedAt:     value.Instrument.UpdatedAt,
		},
		Source: value.Source, Status: value.Status, UpdatedAt: value.UpdatedAt,
	}
}

func poolMemberNotFound(poolID, instrumentID string) error {
	return errors.Join(
		pool.ErrMemberNotFound,
		&apiError{message: "Pool member not found: " + poolID + "/" + instrumentID},
	)
}

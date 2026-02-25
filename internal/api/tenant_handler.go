package api

import (
	"encoding/json"
	"net/http"

	"flowweave/internal/domain/workflow/port"

	"github.com/go-chi/chi/v5"
)

// OrganizationHandler 组织相关的 API 处理器
type OrganizationHandler struct {
	repo port.Repository
}

func NewOrganizationHandler(repo port.Repository) *OrganizationHandler {
	return &OrganizationHandler{repo: repo}
}

func (h *OrganizationHandler) RegisterRoutes(r chi.Router) {
	r.Route("/organizations", func(r chi.Router) {
		r.Post("/", h.CreateOrganization)
		r.Get("/", h.ListOrganizations)
		r.Get("/{id}", h.GetOrganization)
		r.Put("/{id}", h.UpdateOrganization)
		r.Delete("/{id}", h.DeleteOrganization)
	})
}

func (h *OrganizationHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	var org port.Organization
	if err := json.NewDecoder(r.Body).Decode(&org); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if org.Code == "" || org.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}

	if err := h.repo.CreateOrganization(r.Context(), &org); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (h *OrganizationHandler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	org, err := h.repo.GetOrganization(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get organization")
		return
	}
	if org == nil {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (h *OrganizationHandler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.repo.ListOrganizations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (h *OrganizationHandler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var org port.Organization
	if err := json.NewDecoder(r.Body).Decode(&org); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	org.ID = id
	if org.Code == "" || org.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}

	if err := h.repo.UpdateOrganization(r.Context(), &org); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update organization")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (h *OrganizationHandler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.DeleteOrganization(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete organization")
		return
	}
	writeError(w, http.StatusOK, "ok")
}

// TenantHandler 租户相关的 API 处理器
type TenantHandler struct {
	repo port.Repository
}

func NewTenantHandler(repo port.Repository) *TenantHandler {
	return &TenantHandler{repo: repo}
}

func (h *TenantHandler) RegisterRoutes(r chi.Router) {
	r.Route("/tenants", func(r chi.Router) {
		r.Post("/", h.CreateTenant)
		r.Get("/", h.ListTenants)
		r.Get("/{id}", h.GetTenant)
		r.Put("/{id}", h.UpdateTenant)
		r.Delete("/{id}", h.DeleteTenant)
	})
}

func (h *TenantHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	var tenant port.Tenant
	if err := json.NewDecoder(r.Body).Decode(&tenant); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if tenant.OrgID == "" || tenant.Code == "" || tenant.Name == "" {
		writeError(w, http.StatusBadRequest, "org_id, code and name are required")
		return
	}

	if err := h.repo.CreateTenant(r.Context(), &tenant); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create tenant")
		return
	}
	writeJSON(w, http.StatusCreated, tenant)
}

func (h *TenantHandler) GetTenant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tenant, err := h.repo.GetTenant(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	if tenant == nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (h *TenantHandler) ListTenants(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("org_id")
	tenants, err := h.repo.ListTenants(r.Context(), orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tenants")
		return
	}
	writeJSON(w, http.StatusOK, tenants)
}

func (h *TenantHandler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var tenant port.Tenant
	if err := json.NewDecoder(r.Body).Decode(&tenant); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	tenant.ID = id
	if tenant.Code == "" || tenant.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}

	if err := h.repo.UpdateTenant(r.Context(), &tenant); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update tenant")
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (h *TenantHandler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.repo.DeleteTenant(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete tenant")
		return
	}
	writeError(w, http.StatusOK, "ok")
}

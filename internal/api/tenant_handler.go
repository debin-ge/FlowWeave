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
	h.RegisterPublicRoutes(r)
	h.RegisterProtectedRoutes(r)
}

// RegisterPublicRoutes 注册无需鉴权的组织路由（注册/初始化场景）
func (h *OrganizationHandler) RegisterPublicRoutes(r chi.Router) {
	r.Post("/organizations", h.CreateOrganization)
	r.Post("/organizations/", h.CreateOrganization)
}

// RegisterProtectedRoutes 注册需要鉴权的组织路由
func (h *OrganizationHandler) RegisterProtectedRoutes(r chi.Router) {
	h.RegisterProtectedRoutesWithMiddleware(r)
}

// RegisterProtectedRoutesWithMiddleware 注册需要鉴权的组织路由，并附加中间件
func (h *OrganizationHandler) RegisterProtectedRoutesWithMiddleware(r chi.Router, mws ...func(http.Handler) http.Handler) {
	rr := r.With(mws...)
	rr.Get("/organizations", h.ListOrganizations)
	rr.Get("/organizations/", h.ListOrganizations)
	rr.Get("/organizations/{id}", h.GetOrganization)
	rr.Put("/organizations/{id}", h.UpdateOrganization)
	rr.Delete("/organizations/{id}", h.DeleteOrganization)
}

func (h *OrganizationHandler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())

	var org port.Organization
	if err := json.NewDecoder(r.Body).Decode(&org); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if org.Code == "" || org.Name == "" {
		writeError(w, http.StatusBadRequest, "code and name are required")
		return
	}

	if err := h.repo.CreateOrganization(ctx, &org); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create organization")
		return
	}
	writeJSON(w, http.StatusCreated, org)
}

func (h *OrganizationHandler) GetOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())
	id := chi.URLParam(r, "id")
	org, err := h.repo.GetOrganization(ctx, id)
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
	ctx := RepoContextFrom(r.Context())
	orgs, err := h.repo.ListOrganizations(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list organizations")
		return
	}
	writeJSON(w, http.StatusOK, orgs)
}

func (h *OrganizationHandler) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())
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

	existing, err := h.repo.GetOrganization(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get organization")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}

	if err := h.repo.UpdateOrganization(ctx, &org); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update organization")
		return
	}
	writeJSON(w, http.StatusOK, org)
}

func (h *OrganizationHandler) DeleteOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())
	id := chi.URLParam(r, "id")
	existing, err := h.repo.GetOrganization(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get organization")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "organization not found")
		return
	}
	if err := h.repo.DeleteOrganization(ctx, id); err != nil {
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
	h.RegisterPublicRoutes(r)
	h.RegisterProtectedRoutes(r)
}

// RegisterPublicRoutes 注册无需鉴权的租户路由（注册/初始化场景）
func (h *TenantHandler) RegisterPublicRoutes(r chi.Router) {
	r.Post("/tenants", h.CreateTenant)
	r.Post("/tenants/", h.CreateTenant)
}

// RegisterProtectedRoutes 注册需要鉴权的租户路由
func (h *TenantHandler) RegisterProtectedRoutes(r chi.Router) {
	h.RegisterProtectedRoutesWithMiddleware(r)
}

// RegisterProtectedRoutesWithMiddleware 注册需要鉴权的租户路由，并附加中间件
func (h *TenantHandler) RegisterProtectedRoutesWithMiddleware(r chi.Router, mws ...func(http.Handler) http.Handler) {
	rr := r.With(mws...)
	rr.Get("/tenants", h.ListTenants)
	rr.Get("/tenants/", h.ListTenants)
	rr.Get("/tenants/{id}", h.GetTenant)
	rr.Put("/tenants/{id}", h.UpdateTenant)
	rr.Delete("/tenants/{id}", h.DeleteTenant)
}

func (h *TenantHandler) CreateTenant(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())

	var tenant port.Tenant
	if err := json.NewDecoder(r.Body).Decode(&tenant); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if tenant.OrgID == "" || tenant.Code == "" || tenant.Name == "" {
		writeError(w, http.StatusBadRequest, "org_id, code and name are required")
		return
	}

	if err := h.repo.CreateTenant(ctx, &tenant); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create tenant")
		return
	}
	writeJSON(w, http.StatusCreated, tenant)
}

func (h *TenantHandler) GetTenant(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())
	id := chi.URLParam(r, "id")
	tenant, err := h.repo.GetTenant(ctx, id)
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
	ctx := RepoContextFrom(r.Context())
	orgID := r.URL.Query().Get("org_id")
	tenants, err := h.repo.ListTenants(ctx, orgID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tenants")
		return
	}
	writeJSON(w, http.StatusOK, tenants)
}

func (h *TenantHandler) UpdateTenant(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())
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

	existing, err := h.repo.GetTenant(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}

	if err := h.repo.UpdateTenant(ctx, &tenant); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update tenant")
		return
	}
	writeJSON(w, http.StatusOK, tenant)
}

func (h *TenantHandler) DeleteTenant(w http.ResponseWriter, r *http.Request) {
	ctx := RepoContextFrom(r.Context())
	id := chi.URLParam(r, "id")
	existing, err := h.repo.GetTenant(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get tenant")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if err := h.repo.DeleteTenant(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete tenant")
		return
	}
	writeError(w, http.StatusOK, "ok")
}

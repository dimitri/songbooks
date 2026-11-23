package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"app/internal/store"
)

// The roles editor's API, one for a community's roles and a band's
// (store.GroupRoles): kind is "community" or "band", the group is the
// {id} in the path, and every role operation is held to that group's
// roles (a role of another group is 404). The route gates who may call
// it: a community's manage_members, a band's manage_roles.

func groupAndRole(r *http.Request) (groupID, roleID int64, err error) {
	if groupID, err = strconv.ParseInt(r.PathValue("id"), 10, 64); err != nil {
		return 0, 0, err
	}
	if v := r.PathValue("roleID"); v != "" {
		roleID, err = strconv.ParseInt(v, 10, 64)
	}
	return groupID, roleID, err
}

// roleError writes err as the roles API answers it.
func roleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden), errors.Is(err, store.ErrConflict):
		httpError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrInvalid):
		httpError(w, http.StatusBadRequest, err.Error())
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

// handleListRoles is GET /api/{communities,bands}/{id}/roles — any
// current member may see the role list (an invite's target role, the
// roles editor), not just those who can edit it.
func (s *Server) handleListRoles(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, _, err := groupAndRole(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		roles, err := s.store.GroupRoles(kind).List(r.Context(), groupID)
		if err != nil {
			roleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, roles)
	}
}

// handleCreateRole is POST …/roles {"name", "privileges"}: a role of the
// group's own.
func (s *Server) handleCreateRole(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, _, err := groupAndRole(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var req struct {
			Name       string   `json:"name"`
			Privileges []string `json:"privileges"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			httpError(w, http.StatusBadRequest, "name is required")
			return
		}
		roleID, err := s.store.GroupRoles(kind).Create(r.Context(), groupID, req.Name, req.Privileges)
		if err != nil {
			roleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"roleId": roleID})
	}
}

// handleSetRolePrivileges is POST …/roles/{roleID}/privileges
// {"privileges"}: replaces what the role grants — a built-in role's too
// (a group may re-tune what its "member", "core" or "guest" grants).
func (s *Server) handleSetRolePrivileges(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, roleID, err := groupAndRole(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid role id")
			return
		}
		var req struct {
			Privileges []string `json:"privileges"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if err := s.store.GroupRoles(kind).SetPrivileges(r.Context(), groupID, roleID, req.Privileges); err != nil {
			roleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	}
}

// handleRenameRole is POST …/roles/{roleID}/rename {"name"}: a custom
// role only (409 for a built-in).
func (s *Server) handleRenameRole(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, roleID, err := groupAndRole(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid role id")
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
			httpError(w, http.StatusBadRequest, "name is required")
			return
		}
		if err := s.store.GroupRoles(kind).Rename(r.Context(), groupID, roleID, req.Name); err != nil {
			roleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
	}
}

// handleDeleteRole is DELETE …/roles/{roleID}: a custom role no current
// member holds (409 for a built-in, or one still in use).
func (s *Server) handleDeleteRole(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		groupID, roleID, err := groupAndRole(r)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid role id")
			return
		}
		if err := s.store.GroupRoles(kind).Delete(r.Context(), groupID, roleID); err != nil {
			roleError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	}
}

// handleSetBandMemberRole is POST /api/bands/{id}/members/{personID}/role
// {"roleId"}: gives a current member one of the band's roles (their
// core/guest status follows). 409 if no one would be left to manage the
// roles.
func (s *Server) handleSetBandMemberRole(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	personID, err := strconv.ParseInt(r.PathValue("personID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid person id")
		return
	}
	var req struct {
		RoleID int64 `json:"roleId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RoleID == 0 {
		httpError(w, http.StatusBadRequest, "roleId is required")
		return
	}
	if err := s.store.SetBandMemberRole(r.Context(), bandID, personID, req.RoleID, personFromContext(r).ID); err != nil {
		roleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"saved": true})
}

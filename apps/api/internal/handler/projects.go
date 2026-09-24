package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	vtypes "github.com/visionforge/visionforge/packages/types"
)

type projectReq struct {
	Name        string `json:"name" binding:"required,min=1,max=120"`
	Description string `json:"description" binding:"max=2000"`
}
type projectUpdateReq struct {
	Name        *string `json:"name" binding:"omitempty,min=1,max=120"`
	Description *string `json:"description" binding:"omitempty,max=2000"`
}

func (h *Handler) ListProjects(c *gin.Context) {
	uid := currentUserID(c)
	cursor := c.Query("cursor")
	limit := parseLimit(c, 20, 100)
	list, next, hasMore, err := h.s.Projects.List(c.Request.Context(), uid, cursor, limit)
	if err != nil {
		fail(c, err)
		return
	}
	okMeta(c, http.StatusOK, list, pagMeta(next, hasMore, requestID(c)))
}

func (h *Handler) CreateProject(c *gin.Context) {
	var req projectReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, errBind(err))
		return
	}
	p, err := h.s.Projects.Create(c.Request.Context(), actorFrom(c), req.Name, req.Description)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusCreated, p)
}

func (h *Handler) GetProject(c *gin.Context) {
	p, err := h.s.Projects.Get(c.Request.Context(), currentUserID(c), userRole(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusOK, p)
}

func (h *Handler) UpdateProject(c *gin.Context) {
	var req projectUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, errBind(err))
		return
	}
	p, err := h.s.Projects.Update(c.Request.Context(), actorFrom(c), c.Param("id"), req.Name, req.Description)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusOK, p)
}

func (h *Handler) DeleteProject(c *gin.Context) {
	if err := h.s.Projects.Delete(c.Request.Context(), actorFrom(c), c.Param("id")); err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusOK, gin.H{"deleted": true})
}

func userRole(c *gin.Context) string {
	v, _ := c.Get("user_role")
	s, _ := v.(string)
	return s
}

func pagMeta(next string, hasMore bool, rid string) *vtypes.Meta {
	return &vtypes.Meta{NextCursor: next, HasMore: hasMore, RequestID: rid}
}

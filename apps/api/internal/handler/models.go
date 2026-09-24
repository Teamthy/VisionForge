package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	vtypes "github.com/visionforge/visionforge/packages/types"
)

type createModelReq struct {
	Name        string           `json:"name" binding:"required,min=1,max=100"`
	Description string           `json:"description" binding:"max=500"`
	TaskType    vtypes.TaskType  `json:"task_type" binding:"required"`
}

type createVersionReq struct {
	Version     string             `json:"version" binding:"required"`
	ArtifactURI string             `json:"artifact_uri" binding:"required"`
	Runtime     vtypes.Runtime     `json:"runtime" binding:"required"`
	Status      vtypes.ModelVersionStatus `json:"status" binding:"required"`
	Metadata    vtypes.JSONB       `json:"metadata,omitempty"`
}

type setVersionStatusReq struct {
	Status vtypes.ModelVersionStatus `json:"status" binding:"required"`
}

func (h *Handler) ListModels(c *gin.Context) {
	cursor := c.Query("cursor")
	limit := parseLimit(c, 20, 100)
	list, next, hasMore, err := h.s.Models.ListModels(c.Request.Context(), cursor, limit)
	if err != nil { fail(c, err); return }
	okMeta(c, http.StatusOK, list, pagMeta(next, hasMore, requestID(c)))
}

func (h *Handler) CreateModel(c *gin.Context) {
	var req createModelReq
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, errBind(err)); return }
	m, err := h.s.Models.CreateModel(c.Request.Context(), actorFrom(c), req.Name, req.Description, req.TaskType)
	if err != nil { fail(c, err); return }
	ok(c, http.StatusCreated, m)
}

func (h *Handler) GetModel(c *gin.Context) {
	m, err := h.s.Models.GetModel(c.Request.Context(), c.Param("id"))
	if err != nil { fail(c, err); return }
	ok(c, http.StatusOK, m)
}

func (h *Handler) ListModelVersions(c *gin.Context) {
	versions, err := h.s.Models.ListVersions(c.Request.Context(), c.Param("id"))
	if err != nil { fail(c, err); return }
	ok(c, http.StatusOK, versions)
}

func (h *Handler) CreateModelVersion(c *gin.Context) {
	var req createVersionReq
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, errBind(err)); return }
	mv, err := h.s.Models.CreateVersion(c.Request.Context(), actorFrom(c), c.Param("id"), req.Version, req.ArtifactURI, req.Runtime, req.Status, req.Metadata)
	if err != nil { fail(c, err); return }
	ok(c, http.StatusCreated, mv)
}

func (h *Handler) SetModelVersionStatus(c *gin.Context) {
	var req setVersionStatusReq
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, errBind(err)); return }
	if err := h.s.Models.SetVersionStatus(c.Request.Context(), actorFrom(c), c.Param("id"), req.Status); err != nil {
		fail(c, err); return
	}
	ok(c, http.StatusOK, gin.H{"updated": true})
}

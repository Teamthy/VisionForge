package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	vtypes "github.com/visionforge/visionforge/packages/types"
)

type inferenceReq struct {
	AssetID        string          `json:"asset_id" binding:"required"`
	ModelVersionID string          `json:"model_version_id" binding:"required"`
	Priority       vtypes.Priority `json:"priority"`
	Async          bool            `json:"async"`
}

// SyncInference runs a blocking inference (project id from path).
func (h *Handler) SyncInference(c *gin.Context) {
	pid := c.Param("id")
	var req inferenceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, errBind(err))
		return
	}
	j, res, err := h.s.Inference.CreateSync(c.Request.Context(), actorFrom(c), pid, req.AssetID, req.ModelVersionID)
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusOK, gin.H{"job": j, "result": res})
}

// AsyncInference enqueues a job (project id from path). Honors Idempotency-Key.
func (h *Handler) AsyncInference(c *gin.Context) {
	pid := c.Param("id")
	var req inferenceReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, errBind(err))
		return
	}
	var idem *string
	if k := strings.TrimSpace(c.GetHeader("Idempotency-Key")); k != "" {
		idem = &k
	}
	prio := req.Priority
	if prio == 0 {
		prio = vtypes.PriorityNormal
	}
	j, err := h.s.Inference.CreateAsync(c.Request.Context(), actorFrom(c), pid, req.AssetID, req.ModelVersionID, prio, idem)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Location", "/api/v1/jobs/"+j.ID)
	ok(c, http.StatusAccepted, j)
}

// CreateInference handles the legacy /inference endpoint (project_id in body).
func (h *Handler) CreateInference(c *gin.Context) {
	var body struct {
		ProjectID      string          `json:"project_id" binding:"required"`
		AssetID        string          `json:"asset_id" binding:"required"`
		ModelVersionID string          `json:"model_version_id" binding:"required"`
		Priority       vtypes.Priority `json:"priority"`
		Async          bool            `json:"async"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, errBind(err))
		return
	}
	if !body.Async {
		j, res, err := h.s.Inference.CreateSync(c.Request.Context(), actorFrom(c), body.ProjectID, body.AssetID, body.ModelVersionID)
		if err != nil {
			fail(c, err)
			return
		}
		ok(c, http.StatusOK, gin.H{"job": j, "result": res})
		return
	}
	var idem *string
	if k := strings.TrimSpace(c.GetHeader("Idempotency-Key")); k != "" {
		idem = &k
	}
	prio := body.Priority
	if prio == 0 {
		prio = vtypes.PriorityNormal
	}
	j, err := h.s.Inference.CreateAsync(c.Request.Context(), actorFrom(c), body.ProjectID, body.AssetID, body.ModelVersionID, prio, idem)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Location", "/api/v1/jobs/"+j.ID)
	ok(c, http.StatusAccepted, j)
}

func (h *Handler) GetJob(c *gin.Context) {
	j, err := h.s.Inference.Get(c.Request.Context(), currentUserID(c), userRole(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusOK, j)
}

func (h *Handler) GetJobResult(c *gin.Context) {
	r, err := h.s.Inference.GetResult(c.Request.Context(), currentUserID(c), userRole(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusOK, r)
}

func (h *Handler) ListProjectJobs(c *gin.Context) {
	pid := c.Param("id")
	statuses := []vtypes.JobStatus{}
	for _, s := range c.QueryArray("status") {
		statuses = append(statuses, vtypes.JobStatus(s))
	}
	cursor := c.Query("cursor")
	limit := parseLimit(c, 20, 100)
	list, next, hasMore, err := h.s.Inference.List(c.Request.Context(), currentUserID(c), userRole(c), pid, statuses, cursor, limit)
	if err != nil {
		fail(c, err)
		return
	}
	okMeta(c, http.StatusOK, list, pagMeta(next, hasMore, requestID(c)))
}

func (h *Handler) RetryJob(c *gin.Context) {
	j, err := h.s.Inference.Retry(c.Request.Context(), actorFrom(c), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusAccepted, j)
}

func (h *Handler) DashboardStats(c *gin.Context) {
	s, err := h.s.Inference.Stats(c.Request.Context(), currentUserID(c))
	if err != nil {
		fail(c, err)
		return
	}
	ok(c, http.StatusOK, s)
}

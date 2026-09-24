package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type presignReq struct {
	Filename    string `json:"filename" binding:"required"`
	ContentType string `json:"content_type" binding:"required"`
	SizeBytes   int64  `json:"size_bytes" binding:"required,min=1"`
}

func (h *Handler) PresignAsset(c *gin.Context) {
	pid := c.Param("id")
	var req presignReq
	if err := c.ShouldBindJSON(&req); err != nil { fail(c, errBind(err)); return }
	a, url, err := h.s.Assets.PresignUpload(c.Request.Context(), actorFrom(c), pid, req.Filename, req.ContentType, req.SizeBytes)
	if err != nil { fail(c, err); return }
	ok(c, http.StatusCreated, gin.H{
		"asset":     a,
		"upload_url": url,
		"expires_in": int(15 * time.Minute / time.Second),
	})
}

func (h *Handler) ConfirmAsset(c *gin.Context) {
	var body struct{ AssetID string `json:"asset_id" binding:"required"` }
	if err := c.ShouldBindJSON(&body); err != nil { fail(c, errBind(err)); return }
	a, err := h.s.Assets.ConfirmUpload(c.Request.Context(), actorFrom(c), body.AssetID)
	if err != nil { fail(c, err); return }
	ok(c, http.StatusOK, a)
}

func (h *Handler) UploadAsset(c *gin.Context) {
	pid := c.Param("id")
	fh, err := c.FormFile("file")
	if err != nil { fail(c, errBind(err)); return }
	a, err := h.s.Assets.UploadDirect(c.Request.Context(), actorFrom(c), pid, fh)
	if err != nil { fail(c, err); return }
	ok(c, http.StatusCreated, a)
}

func (h *Handler) GetAsset(c *gin.Context) {
	a, err := h.s.Assets.Get(c.Request.Context(), currentUserID(c), userRole(c), c.Param("id"))
	if err != nil { fail(c, err); return }
	ok(c, http.StatusOK, a)
}

func (h *Handler) ListAssets(c *gin.Context) {
	pid := c.Param("id")
	cursor := c.Query("cursor")
	limit := parseLimit(c, 20, 100)
	list, next, hasMore, err := h.s.Assets.ListByProject(c.Request.Context(), currentUserID(c), userRole(c), pid, cursor, limit)
	if err != nil { fail(c, err); return }
	okMeta(c, http.StatusOK, list, pagMeta(next, hasMore, requestID(c)))
}

func (h *Handler) DeleteAsset(c *gin.Context) {
	if err := h.s.Assets.Delete(c.Request.Context(), actorFrom(c), c.Param("id")); err != nil {
		fail(c, err); return
	}
	ok(c, http.StatusOK, gin.H{"deleted": true})
}

func (h *Handler) DownloadAsset(c *gin.Context) {
	url, a, err := h.s.Assets.DownloadURL(c.Request.Context(), currentUserID(c), userRole(c), c.Param("id"), 10*time.Minute)
	if err != nil { fail(c, err); return }
	ok(c, http.StatusOK, gin.H{"asset": a, "download_url": url})
}

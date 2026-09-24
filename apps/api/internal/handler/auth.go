package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type registerReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8"`
	Name     string `json:"name" binding:"required"`
}
type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}
type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func (h *Handler) Register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, errBind(err)); return
	}
	u, access, refresh, exp, err := h.s.Auth.Register(c.Request.Context(), req.Email, req.Password, req.Name, clientIP(c), c.Request.UserAgent())
	if err != nil { fail(c, err); return }
	setRefreshCookie(c, refresh, exp)
	ok(c, http.StatusCreated, gin.H{"user": u, "access_token": access, "expires_at": exp})
}

func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, errBind(err)); return
	}
	u, access, refresh, exp, err := h.s.Auth.Login(c.Request.Context(), req.Email, req.Password, clientIP(c), c.Request.UserAgent())
	if err != nil { fail(c, err); return }
	setRefreshCookie(c, refresh, exp)
	ok(c, http.StatusOK, gin.H{"user": u, "access_token": access, "expires_at": exp})
}

func (h *Handler) Refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		// fall back to cookie
		if ck, err := c.Cookie("vf_refresh"); err == nil { req.RefreshToken = ck }
		if req.RefreshToken == "" { fail(c, errBind(err)); return }
	}
	access, refresh, exp, err := h.s.Auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil { fail(c, err); return }
	setRefreshCookie(c, refresh, exp)
	ok(c, http.StatusOK, gin.H{"access_token": access, "expires_at": exp})
}

func (h *Handler) Logout(c *gin.Context) {
	tok, _ := c.Cookie("vf_refresh")
	var req refreshReq
	_ = c.ShouldBindJSON(&req)
	if req.RefreshToken != "" { tok = req.RefreshToken }
	uid := currentUserID(c)
	if err := h.s.Auth.Logout(c.Request.Context(), tok, uid, clientIP(c), c.Request.UserAgent()); err != nil {
		fail(c, err); return
	}
	c.SetCookie("vf_refresh", "", -1, "/", "", false, true)
	ok(c, http.StatusOK, gin.H{"logged_out": true})
}

func (h *Handler) Me(c *gin.Context) {
	u, err := h.s.Auth.Me(c.Request.Context(), currentUserID(c))
	if err != nil { fail(c, err); return }
	ok(c, http.StatusOK, u)
}

func setRefreshCookie(c *gin.Context, val string, exp time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     "vf_refresh",
		Value:    val,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Request.TLS != nil,
		Expires:  exp,
	})
}

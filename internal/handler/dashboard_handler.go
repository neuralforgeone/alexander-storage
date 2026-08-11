// Package handler provides HTTP handlers for Alexander Storage.
package handler

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/prn-tf/alexander-storage/internal/domain"
	"github.com/prn-tf/alexander-storage/internal/middleware"
	"github.com/prn-tf/alexander-storage/internal/service"
)

//go:embed templates/*.html templates/*.css
var templateFS embed.FS

// DashboardHandler handles web dashboard requests.
type DashboardHandler struct {
	sessionService   *service.SessionService
	userService      *service.UserService
	bucketService    *service.BucketService
	objectService    *service.ObjectService
	lifecycleService *service.LifecycleService
	templates        *template.Template
	logger           zerolog.Logger
}

// DashboardConfig contains configuration for the dashboard.
type DashboardConfig struct {
	SessionService   *service.SessionService
	UserService      *service.UserService
	BucketService    *service.BucketService
	ObjectService    *service.ObjectService
	LifecycleService *service.LifecycleService
	Logger           zerolog.Logger
}

// NewDashboardHandler creates a new dashboard handler.
func NewDashboardHandler(cfg DashboardConfig) (*DashboardHandler, error) {
	funcMap := template.FuncMap{
		"formatBytes": formatBytes,
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.UTC().Format("2006-01-02 15:04")
		},
		"urlquery": url.QueryEscape,
		"pathJoin": path.Join,
		"hasPrefix": strings.HasPrefix,
		"trimSuffix": strings.TrimSuffix,
		"isPreviewable": isPreviewableContentType,
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}

	return &DashboardHandler{
		sessionService:   cfg.SessionService,
		userService:      cfg.UserService,
		bucketService:    cfg.BucketService,
		objectService:    cfg.ObjectService,
		lifecycleService: cfg.LifecycleService,
		templates:        tmpl,
		logger:           cfg.Logger.With().Str("handler", "dashboard").Logger(),
	}, nil
}

// =============================================================================
// Template data
// =============================================================================

// PageData contains common page data.
type PageData struct {
	Title     string
	Username  string
	Error     string
	Success   string
	CSRFToken string
	ActiveNav string
}

// LoginPageData contains login page data.
type LoginPageData struct {
	PageData
}

// DashboardPageData contains main dashboard page data.
type DashboardPageData struct {
	PageData
	Buckets []*domain.Bucket
}

// BucketDetailPageData contains bucket browser page data.
type BucketDetailPageData struct {
	PageData
	Bucket         *domain.Bucket
	Prefix         string
	ParentPrefix   string
	Objects        []service.ObjectInfo
	CommonPrefixes []string
	LifecycleRules []*domain.LifecycleRule
	IsTruncated    bool
}

// ObjectPreviewPageData contains object metadata/preview page data.
type ObjectPreviewPageData struct {
	PageData
	Bucket      string
	Key         string
	Size        int64
	ContentType string
	ETag        string
	LastModified time.Time
	VersionID   string
	PreviewText string
	IsImage     bool
	IsText      bool
	TooLarge    bool
}

// UsersPageData contains users management page data.
type UsersPageData struct {
	PageData
	Users []*domain.User
}

// =============================================================================
// Route registration
// =============================================================================

// Handler returns an http.Handler for all /dashboard routes.
func (h *DashboardHandler) Handler() http.Handler {
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

// RegisterRoutes registers dashboard routes on a chi router.
func (h *DashboardHandler) RegisterRoutes(r chi.Router) {
	r.Get("/dashboard/static/dashboard.css", h.handleCSS)

	r.Get("/dashboard/login", h.handleLoginPage)
	r.Post("/dashboard/login", h.handleLogin)
	r.Post("/dashboard/logout", h.handleLogout)

	r.Get("/dashboard", h.handleDashboard)
	r.Get("/dashboard/", h.handleDashboard)

	r.Post("/dashboard/buckets", h.handleCreateBucket)
	r.Get("/dashboard/buckets/{name}", h.handleBucketDetail)
	r.Post("/dashboard/buckets/{name}/acl", h.handleUpdateBucketACL)
	r.Post("/dashboard/buckets/{name}/upload", h.handleUploadObject)
	r.Get("/dashboard/buckets/{name}/object", h.handleObjectPage)
	r.Get("/dashboard/buckets/{name}/raw", h.handleObjectRaw)

	r.Post("/dashboard/buckets/{name}/lifecycle", h.handleCreateLifecycleRule)
	r.Post("/dashboard/buckets/{name}/lifecycle/{ruleId}/delete", h.handleDeleteLifecycleRule)

	r.Get("/dashboard/users", h.handleUserList)
	r.Post("/dashboard/users", h.handleCreateUser)
	r.Post("/dashboard/users/{id}/delete", h.handleDeleteUser)
}

// =============================================================================
// Auth
// =============================================================================

func (h *DashboardHandler) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if _, err := h.getSession(r); err == nil {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	h.render(w, "login.html", LoginPageData{
		PageData: PageData{
			Title:     "Sign in · Alexander",
			CSRFToken: middleware.TokenFromContext(r.Context()),
		},
	})
}

func (h *DashboardHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.renderLoginError(w, r, "Invalid form data")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		h.renderLoginError(w, r, "Username and password are required")
		return
	}

	output, err := h.sessionService.Login(r.Context(), service.LoginInput{
		Username:  username,
		Password:  password,
		IPAddress: r.RemoteAddr,
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		h.logger.Debug().Err(err).Str("username", username).Msg("login failed")
		h.renderLoginError(w, r, "Invalid username or password")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    output.Session.Token,
		Path:     "/dashboard",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour / time.Second),
	})

	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *DashboardHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("session"); err == nil {
		_ = h.sessionService.Logout(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/dashboard",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/dashboard/login", http.StatusFound)
}

// =============================================================================
// Dashboard home
// =============================================================================

func (h *DashboardHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}

	buckets, err := h.bucketService.ListBuckets(r.Context(), service.ListBucketsInput{
		OwnerID: session.UserID,
	})
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list buckets")
		h.renderError(w, r, "Failed to load buckets", session.Username)
		return
	}

	h.render(w, "dashboard.html", DashboardPageData{
		PageData: PageData{
			Title:     "Buckets · Alexander",
			Username:  session.Username,
			CSRFToken: middleware.TokenFromContext(r.Context()),
			ActiveNav: "buckets",
			Success:   r.URL.Query().Get("ok"),
			Error:     r.URL.Query().Get("err"),
		},
		Buckets: buckets.Buckets,
	})
}

func (h *DashboardHandler) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard?err="+url.QueryEscape("Invalid form"), http.StatusFound)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	_, err = h.bucketService.CreateBucket(r.Context(), service.CreateBucketInput{
		OwnerID: session.UserID,
		Name:    name,
		Region:  "us-east-1",
	})
	if err != nil {
		http.Redirect(w, r, "/dashboard?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(name)+"?ok="+url.QueryEscape("Bucket created"), http.StatusFound)
}

// =============================================================================
// Bucket browser
// =============================================================================

func (h *DashboardHandler) handleBucketDetail(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}

	bucketName := chi.URLParam(r, "name")
	prefix := r.URL.Query().Get("prefix")

	bucketOut, err := h.bucketService.GetBucket(r.Context(), service.GetBucketInput{
		Name:    bucketName,
		OwnerID: session.UserID,
	})
	if err != nil {
		h.renderError(w, r, "Bucket not found", session.Username)
		return
	}

	listOut, err := h.objectService.ListObjects(r.Context(), service.ListObjectsInput{
		BucketName: bucketName,
		Prefix:     prefix,
		Delimiter:  "/",
		MaxKeys:    500,
		OwnerID:    session.UserID,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("bucket", bucketName).Msg("list objects failed")
		h.renderError(w, r, "Failed to list objects", session.Username)
		return
	}

	var rules []*domain.LifecycleRule
	if h.lifecycleService != nil {
		rules, err = h.lifecycleService.GetRules(r.Context(), bucketName)
		if err != nil {
			h.logger.Debug().Err(err).Msg("lifecycle rules unavailable")
			rules = nil
		}
	}

	h.render(w, "bucket_detail.html", BucketDetailPageData{
		PageData: PageData{
			Title:     bucketName + " · Alexander",
			Username:  session.Username,
			CSRFToken: middleware.TokenFromContext(r.Context()),
			ActiveNav: "buckets",
			Success:   r.URL.Query().Get("ok"),
			Error:     r.URL.Query().Get("err"),
		},
		Bucket:         bucketOut.Bucket,
		Prefix:         prefix,
		ParentPrefix:   parentPrefix(prefix),
		Objects:        listOut.Contents,
		CommonPrefixes: listOut.CommonPrefixes,
		LifecycleRules: rules,
		IsTruncated:    listOut.IsTruncated,
	})
}

func (h *DashboardHandler) handleUpdateBucketACL(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}
	bucketName := chi.URLParam(r, "name")
	acl := domain.BucketACL(r.FormValue("acl"))
	if err := h.bucketService.UpdateBucketACL(r.Context(), service.UpdateBucketACLInput{
		Name:    bucketName,
		OwnerID: session.UserID,
		ACL:     acl,
	}); err != nil {
		http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?ok="+url.QueryEscape("ACL updated"), http.StatusFound)
}

func (h *DashboardHandler) handleUploadObject(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}

	bucketName := chi.URLParam(r, "name")
	// 64 MiB form memory; rest to temp files
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?err="+url.QueryEscape("Upload parse failed"), http.StatusFound)
		return
	}

	prefix := r.FormValue("prefix")
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?prefix="+url.QueryEscape(prefix)+"&err="+url.QueryEscape("No file selected"), http.StatusFound)
		return
	}
	defer file.Close()

	keyName := r.FormValue("key")
	if keyName == "" {
		keyName = path.Base(header.Filename)
	}
	key := prefix + keyName
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err = h.objectService.PutObject(r.Context(), service.PutObjectInput{
		BucketName:  bucketName,
		Key:         key,
		Body:        file,
		Size:        header.Size,
		ContentType: contentType,
		OwnerID:     session.UserID,
	})
	if err != nil {
		http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?prefix="+url.QueryEscape(prefix)+"&err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?prefix="+url.QueryEscape(prefix)+"&ok="+url.QueryEscape("Uploaded "+key), http.StatusFound)
}

// =============================================================================
// Object view / download
// =============================================================================

func (h *DashboardHandler) handleObjectPage(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}

	bucketName := chi.URLParam(r, "name")
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName), http.StatusFound)
		return
	}

	head, err := h.objectService.HeadObject(r.Context(), service.HeadObjectInput{
		BucketName: bucketName,
		Key:        key,
		OwnerID:    session.UserID,
	})
	if err != nil {
		h.renderError(w, r, "Object not found", session.Username)
		return
	}

	data := ObjectPreviewPageData{
		PageData: PageData{
			Title:     key + " · Alexander",
			Username:  session.Username,
			CSRFToken: middleware.TokenFromContext(r.Context()),
			ActiveNav: "buckets",
		},
		Bucket:       bucketName,
		Key:          key,
		Size:         head.ContentLength,
		ContentType:  head.ContentType,
		ETag:         head.ETag,
		LastModified: head.LastModified,
		VersionID:    head.VersionID,
		IsImage:      strings.HasPrefix(head.ContentType, "image/"),
		IsText:       isTextContentType(head.ContentType),
	}

	const maxPreview = 256 * 1024
	if data.IsText && head.ContentLength > 0 && head.ContentLength <= maxPreview {
		out, err := h.objectService.GetObject(r.Context(), service.GetObjectInput{
			BucketName: bucketName,
			Key:         key,
			OwnerID:     session.UserID,
		})
		if err == nil {
			defer out.Body.Close()
			b, readErr := io.ReadAll(io.LimitReader(out.Body, maxPreview+1))
			if readErr == nil && len(b) <= maxPreview {
				data.PreviewText = string(b)
			} else {
				data.TooLarge = true
			}
		}
	} else if data.IsText && head.ContentLength > maxPreview {
		data.TooLarge = true
	}

	h.render(w, "object.html", data)
}

func (h *DashboardHandler) handleObjectRaw(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	bucketName := chi.URLParam(r, "name")
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	disposition := r.URL.Query().Get("disposition")
	if disposition != "attachment" {
		disposition = "inline"
	}

	out, err := h.objectService.GetObject(r.Context(), service.GetObjectInput{
		BucketName: bucketName,
		Key:        key,
		OwnerID:    session.UserID,
	})
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer out.Body.Close()

	w.Header().Set("Content-Type", out.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(out.ContentLength, 10))
	w.Header().Set("ETag", out.ETag)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, path.Base(key)))
	if out.VersionID != "" {
		w.Header().Set("x-amz-version-id", out.VersionID)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, out.Body)
}

// =============================================================================
// Lifecycle
// =============================================================================

func (h *DashboardHandler) handleCreateLifecycleRule(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}
	if h.lifecycleService == nil {
		http.Error(w, "lifecycle not available", http.StatusNotImplemented)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	bucketName := chi.URLParam(r, "name")
	if _, err := h.bucketService.GetBucket(r.Context(), service.GetBucketInput{
		Name: bucketName, OwnerID: session.UserID,
	}); err != nil {
		http.Redirect(w, r, "/dashboard?err="+url.QueryEscape("Bucket not found"), http.StatusFound)
		return
	}

	expirationDays, _ := strconv.Atoi(r.FormValue("expiration_days"))
	_, err = h.lifecycleService.CreateRule(r.Context(), service.CreateRuleInput{
		BucketName:     bucketName,
		RuleID:         r.FormValue("rule_id"),
		Prefix:         r.FormValue("prefix"),
		ExpirationDays: expirationDays,
		Status:         "Enabled",
	})
	if err != nil {
		http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?ok="+url.QueryEscape("Lifecycle rule added"), http.StatusFound)
}

func (h *DashboardHandler) handleDeleteLifecycleRule(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}
	if h.lifecycleService == nil {
		http.Error(w, "lifecycle not available", http.StatusNotImplemented)
		return
	}
	bucketName := chi.URLParam(r, "name")
	ruleID := chi.URLParam(r, "ruleId")
	_ = session
	if err := h.lifecycleService.DeleteRuleByName(r.Context(), bucketName, ruleID); err != nil {
		http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard/buckets/"+url.PathEscape(bucketName)+"?ok="+url.QueryEscape("Rule deleted"), http.StatusFound)
}

// =============================================================================
// Users
// =============================================================================

func (h *DashboardHandler) handleUserList(w http.ResponseWriter, r *http.Request) {
	session, err := h.getSession(r)
	if err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}

	output, err := h.userService.List(r.Context(), service.ListUsersInput{Limit: 100})
	if err != nil {
		h.renderError(w, r, "Failed to load users", session.Username)
		return
	}

	h.render(w, "users.html", UsersPageData{
		PageData: PageData{
			Title:     "Users · Alexander",
			Username:  session.Username,
			CSRFToken: middleware.TokenFromContext(r.Context()),
			ActiveNav: "users",
			Success:   r.URL.Query().Get("ok"),
			Error:     r.URL.Query().Get("err"),
		},
		Users: output.Users,
	})
}

func (h *DashboardHandler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if _, err := h.getSession(r); err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard/users?err="+url.QueryEscape("Invalid form"), http.StatusFound)
		return
	}
	_, err := h.userService.Create(r.Context(), service.CreateUserInput{
		Username: r.FormValue("username"),
		Email:    r.FormValue("email"),
		Password: r.FormValue("password"),
	})
	if err != nil {
		http.Redirect(w, r, "/dashboard/users?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard/users?ok="+url.QueryEscape("User created"), http.StatusFound)
}

func (h *DashboardHandler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if _, err := h.getSession(r); err != nil {
		http.Redirect(w, r, "/dashboard/login", http.StatusFound)
		return
	}
	userID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/dashboard/users?err="+url.QueryEscape("Invalid user"), http.StatusFound)
		return
	}
	if err := h.userService.Delete(r.Context(), userID); err != nil {
		http.Redirect(w, r, "/dashboard/users?err="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/dashboard/users?ok="+url.QueryEscape("User deleted"), http.StatusFound)
}

// =============================================================================
// Static + helpers
// =============================================================================

func (h *DashboardHandler) handleCSS(w http.ResponseWriter, r *http.Request) {
	b, err := templateFS.ReadFile("templates/dashboard.css")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(b)
}

type sessionInfo struct {
	UserID   int64
	Username string
}

func (h *DashboardHandler) getSession(r *http.Request) (*sessionInfo, error) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, err
	}
	session, user, err := h.sessionService.ValidateSession(r.Context(), cookie.Value)
	if err != nil {
		return nil, err
	}
	return &sessionInfo{UserID: session.UserID, Username: user.Username}, nil
}

func (h *DashboardHandler) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, name, data); err != nil {
		h.logger.Error().Err(err).Str("template", name).Msg("failed to render template")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (h *DashboardHandler) renderError(w http.ResponseWriter, r *http.Request, message, username string) {
	h.render(w, "error.html", PageData{
		Title:     "Error · Alexander",
		Username:  username,
		Error:     message,
		CSRFToken: middleware.TokenFromContext(r.Context()),
	})
}

func (h *DashboardHandler) renderLoginError(w http.ResponseWriter, r *http.Request, message string) {
	h.render(w, "login.html", LoginPageData{
		PageData: PageData{
			Title:     "Sign in · Alexander",
			Error:     message,
			CSRFToken: middleware.TokenFromContext(r.Context()),
		},
	})
}

func parentPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	p := strings.TrimSuffix(prefix, "/")
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return ""
	}
	return p[:i+1]
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func isTextContentType(ct string) bool {
	ct = strings.ToLower(ct)
	if strings.HasPrefix(ct, "text/") {
		return true
	}
	switch {
	case strings.Contains(ct, "json"),
		strings.Contains(ct, "xml"),
		strings.Contains(ct, "javascript"),
		strings.Contains(ct, "yaml"),
		strings.Contains(ct, "toml"),
		strings.Contains(ct, "markdown"),
		ct == "application/x-sh",
		ct == "application/sql":
		return true
	default:
		return false
	}
}

func isPreviewableContentType(ct string) bool {
	return isTextContentType(ct) || strings.HasPrefix(strings.ToLower(ct), "image/")
}

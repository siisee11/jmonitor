package app

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/dev/jmonitor/internal/codexapi"
	"github.com/dev/jmonitor/internal/codexauth"
	"github.com/dev/jmonitor/internal/config"
	"github.com/dev/jmonitor/internal/store"
)

type App struct {
	cfg        config.Config
	store      *store.Store
	codexAPI   *codexapi.Client
	httpServer *http.Server
	dashboard  *template.Template
}

//go:embed templates/*.html
var templateFS embed.FS

func New(cfg config.Config) (*App, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err := st.Migrate(ctx); err != nil {
		st.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	templatePath := path.Join("templates", "dashboard.html")
	templateBytes, err := templateFS.ReadFile(templatePath)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("read dashboard template: %w", err)
	}

	tmpl, err := template.New("dashboard").Parse(string(templateBytes))
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("parse dashboard template: %w", err)
	}

	app := &App{
		cfg:       cfg,
		store:     st,
		codexAPI:  codexapi.New(),
		dashboard: tmpl,
	}
	app.httpServer = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return app, nil
}

func (a *App) Close() {
	a.store.Close()
}

func (a *App) RunPoller(ctx context.Context) {
	a.pollOnce(ctx)

	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollOnce(ctx)
		}
	}
}

func (a *App) pollOnce(ctx context.Context) {
	snapshots, err := codexauth.DiscoverAccountSnapshots(a.cfg.CodexHome)
	if err != nil {
		return
	}

	for _, accountSnapshot := range snapshots {
		capturedAt := time.Now().UTC()
		accountID, err := a.store.UpsertAccount(ctx, accountSnapshot)
		if err != nil {
			continue
		}

		accountCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		usage, err := a.codexAPI.FetchUsage(accountCtx, accountSnapshot.AccessToken, accountSnapshot.ChatGPTAccountID)
		cancel()
		if err != nil {
			_ = a.store.RecordPollFailure(ctx, accountID, capturedAt, err)
			continue
		}
		if err := a.store.RecordPollSuccess(ctx, accountID, usage, capturedAt); err != nil {
			_ = a.store.RecordPollFailure(ctx, accountID, capturedAt, err)
			continue
		}
	}
}

func (a *App) RunHTTP(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		errCh <- a.httpServer.Shutdown(shutdownCtx)
	}()

	err := a.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		select {
		case shutdownErr := <-errCh:
			if shutdownErr != nil {
				return shutdownErr
			}
		default:
		}
	}
	return err
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleDashboard)
	mux.HandleFunc("/api/accounts", a.handleAccounts)
	mux.HandleFunc("/api/accounts/", a.handleAccountHistory)
	mux.HandleFunc("/healthz", a.handleHealthz)
	return mux
}

func (a *App) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := struct {
		PollInterval string
		CodexHome    string
	}{
		PollInterval: a.cfg.PollInterval.String(),
		CodexHome:    a.cfg.CodexHome,
	}
	if err := a.dashboard.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	accounts, err := a.store.ListAccounts(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts,
	})
}

func (a *App) handleAccountHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/accounts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[1] != "history" {
		http.NotFound(w, r)
		return
	}

	accountID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || accountID <= 0 {
		http.Error(w, "invalid account id", http.StatusBadRequest)
		return
	}

	windowName := r.URL.Query().Get("window")
	if windowName == "" {
		windowName = "five_hour"
	}
	limit := 288
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 && parsed <= 5000 {
			limit = parsed
		}
	}

	points, err := a.store.AccountHistory(r.Context(), accountID, windowName, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accountId": accountID,
		"window":    windowName,
		"points":    points,
	})
}

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"pollInterval": a.cfg.PollInterval.String(),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

/*
Alien

# Copyright 2016,2023 James McHugh

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"context"
	crand "crypto/rand"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"errors"
	"html/template"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"alien/db"

	"github.com/exploded/monitor/pkg/logship"
	_ "modernc.org/sqlite"
)

//go:embed db/schema.sql
var schemaSQL string

var (
	sqlDB        *sql.DB
	queries      *db.Queries
	tmplQuestion *template.Template
	tmplIntro    *template.Template
	tmplAbout    *template.Template
	isProd       bool
)

func main() {
	mime.AddExtensionType(".webp", "image/webp")

	var ship *logship.Handler
	monitorURL := os.Getenv("MONITOR_URL")
	monitorKey := os.Getenv("MONITOR_API_KEY")

	if monitorURL != "" && monitorKey != "" {
		ship = logship.New(logship.Options{
			Endpoint: monitorURL + "/api/logs",
			APIKey:   monitorKey,
			App:      "alien",
			Level:    slog.LevelWarn,
		})
		defer ship.Shutdown()

		logger := slog.New(logship.Multi(
			slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}),
			ship,
		))
		slog.SetDefault(logger)
		slog.Warn("alien app started, log shipping active", "endpoint", monitorURL+"/api/logs")
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	}

	isProd = os.Getenv("PROD") == "True"

	var err error
	sqlDB, err = sql.Open("sqlite", "alien.db")
	if err != nil {
		slog.Error("could not open database", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
		slog.Error("could not set WAL mode", "err", err)
		os.Exit(1)
	}
	if _, err := sqlDB.Exec("PRAGMA foreign_keys=ON"); err != nil {
		slog.Error("could not enable foreign keys", "err", err)
		os.Exit(1)
	}

	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		slog.Error("could not create schema", "err", err)
		os.Exit(1)
	}

	// Migration: drop PII columns from answer table (if they still exist).
	// VACUUM reclaims disk space after the drops.
	sqlDB.Exec("ALTER TABLE answer DROP COLUMN submitter")
	sqlDB.Exec("ALTER TABLE answer DROP COLUMN submitteragent")
	sqlDB.Exec("VACUUM")

	queries = db.New(sqlDB)
	slog.Info("database connected")

	path, err := os.Getwd()
	if err != nil {
		slog.Error("failed to get working directory", "err", err)
		os.Exit(1)
	}

	base := filepath.Join(path, "templates", "base.html")
	tmplQuestion = template.Must(template.ParseFiles(base, filepath.Join(path, "templates", "question.html")))
	tmplIntro = template.Must(template.ParseFiles(base, filepath.Join(path, "templates", "intro.html")))
	tmplAbout = template.Must(template.ParseFiles(base, filepath.Join(path, "templates", "about.html")))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8787"
	}

	slog.Info("starting", "path", path, "port", port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", siteroot)
	mux.HandleFunc("/about", about)
	mux.HandleFunc("/intro", intro)
	mux.HandleFunc("/robots.txt", robots)
	mux.Handle("/css/", http.StripPrefix("/css/", noDirListing(http.FileServer(http.Dir(filepath.Join(path, "css"))))))
	mux.Handle("/images/", http.StripPrefix("/images/", noDirListing(http.FileServer(http.Dir(filepath.Join(path, "images"))))))
	mux.HandleFunc("/favicon.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(path, "images", "favicon.png"))
	})
	mux.Handle("/static/", http.StripPrefix("/static/", noDirListing(http.FileServer(http.Dir(filepath.Join(path, "static"))))))

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      requestLogger(securityHeaders(mux)),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}()

	slog.Info("server started", "port", port)
	<-done
	slog.Info("shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "err", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")
		if isProd {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

func noDirListing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") || r.URL.Path == "" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func about(w http.ResponseWriter, r *http.Request) {

	type AboutParams struct {
		Responses int64
	}

	var P AboutParams

	count, err := queries.CountAnswers(r.Context())
	if err != nil {
		slog.Error("about: query failed", "err", err)
	}
	P.Responses = count

	w.Header().Set("Cache-Control", "private, max-age=0, no-cache")
	if err := tmplAbout.ExecuteTemplate(w, "base", P); err != nil {
		slog.Error("about: template failed", "err", err)
	}
}

func intro(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if err := tmplIntro.ExecuteTemplate(w, "base", nil); err != nil {
		slog.Error("intro: template failed", "err", err)
	}
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, "robots.txt")
}

func siteroot(w http.ResponseWriter, r *http.Request) {
	type Pageparam struct {
		Id            int64
		Previous      string
		PreviousValue int64
		PreviousStats string
		Category      string
		Question      string
		Picture       string
		Short         string
		CsrfToken     string
	}

	var P Pageparam
	csrfToken, err := ensureCSRFCookie(w, r)
	if err != nil {
		slog.Error("csrf cookie failed", "err", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		return
	}
	P.CsrfToken = csrfToken

	switch r.Method {
	case http.MethodGet:
		q := r.URL.Query()
		squestion := q.Get("question")
		if squestion == "" {
			http.Redirect(w, r, "/intro", http.StatusFound)
			return
		}

		idold, err := strconv.ParseInt(squestion, 10, 64)
		if err != nil {
			slog.Warn("invalid question param", "err", err)
			http.Error(w, "Invalid question", http.StatusBadRequest)
			return
		}
		if idold < 0 || idold >= 60 {
			http.Error(w, "Invalid question", http.StatusBadRequest)
			return
		}

		if idold == 0 {
			rq, err := queries.GetRandomQuestion(r.Context())
			if err != nil {
				slog.Error("random question failed", "err", err)
				http.Error(w, "Unable to load question", http.StatusInternalServerError)
				return
			}
			P.Id = rq.ID
		} else {
			P.Id = idold
		}
	case http.MethodPost:
		if !voteLimiter.Allow(clientIP(r.RemoteAddr)) {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			slog.Warn("form parse failed", "err", err)
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}
		if err := verifyCSRF(r); err != nil {
			slog.Warn("csrf verification failed", "err", err)
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		squestion := r.PostFormValue("question")
		idold, err := strconv.ParseInt(squestion, 10, 64)
		if err != nil {
			slog.Warn("invalid question param", "err", err)
			http.Error(w, "Invalid question", http.StatusBadRequest)
			return
		}
		if idold <= 0 || idold >= 60 {
			http.Error(w, "Invalid question", http.StatusBadRequest)
			return
		}

		answer := r.PostFormValue("answer")
		if answer != "yes" && answer != "no" {
			http.Error(w, "Invalid answer", http.StatusBadRequest)
			return
		}

		tx, err := sqlDB.BeginTx(r.Context(), nil)
		if err != nil {
			slog.Error("begin tx failed", "err", err)
			http.Error(w, "Unable to process vote", http.StatusInternalServerError)
			return
		}
		defer tx.Rollback()

		qtx := db.New(tx)
		answerBit := int64(0)
		if answer == "yes" {
			answerBit = 1
			err = qtx.IncrementYes(r.Context(), idold)
		} else {
			err = qtx.IncrementNo(r.Context(), idold)
		}
		if err != nil {
			slog.Error("vote update failed", "err", err, "question", idold)
			http.Error(w, "Unable to process vote", http.StatusInternalServerError)
			return
		}

		err = qtx.InsertAnswer(r.Context(), db.InsertAnswerParams{
			Question: idold,
			Answer:   answerBit,
		})
		if err != nil {
			slog.Error("vote insert failed", "err", err, "question", idold)
			http.Error(w, "Unable to process vote", http.StatusInternalServerError)
			return
		}

		if err := tx.Commit(); err != nil {
			slog.Error("commit failed", "err", err, "question", idold)
			http.Error(w, "Unable to process vote", http.StatusInternalServerError)
			return
		}

		stats, err := queries.GetQuestionStats(r.Context(), idold)
		if err != nil {
			slog.Error("select stats failed", "err", err, "question", idold)
		} else {
			P.Previous = stats.Short
			yes := stats.Yes
			no := stats.No
			if yes+no > 0 {
				P.PreviousValue = int64(100. * (float64(yes) / float64(yes+no)))
			}
			P.PreviousStats = "Yes:" + strconv.FormatInt(yes, 10) + " No:" + strconv.FormatInt(no, 10) + " Total votes:" + strconv.FormatInt(yes+no, 10)
		}

		P.Id = idold + 1
		if P.Id == 60 {
			P.Id = 1
		}
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	question, err := queries.GetQuestion(r.Context(), P.Id)
	if err != nil {
		slog.Error("select question failed", "err", err, "question", P.Id)
		http.Error(w, "Unable to load question", http.StatusInternalServerError)
		return
	}
	P.Category = question.Category
	P.Question = question.Question
	P.Picture = question.Picture
	P.Short = question.Short

	w.Header().Set("Cache-Control", "private, max-age=0, no-cache")

	if err := tmplQuestion.ExecuteTemplate(w, "base", P); err != nil {
		slog.Error("question: template failed", "err", err)
	}

}

const (
	csrfCookieName = "csrf_token"
	csrfFormField  = "csrf_token"
)

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request) (string, error) {
	if token, ok := getCSRFCookie(r); ok {
		return token, nil
	}

	token, err := newToken()
	if err != nil {
		return "", err
	}

	setCSRFCookie(w, r, token)
	return token, nil
}

func verifyCSRF(r *http.Request) error {
	cookieToken, ok := getCSRFCookie(r)
	if !ok {
		return errors.New("missing csrf cookie")
	}

	formToken := r.PostFormValue(csrfFormField)
	if formToken == "" {
		return errors.New("missing csrf token")
	}

	if subtle.ConstantTimeCompare([]byte(cookieToken), []byte(formToken)) != 1 {
		return errors.New("csrf token mismatch")
	}

	return nil
}

func getCSRFCookie(r *http.Request) (string, bool) {
	c, err := r.Cookie(csrfCookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

func setCSRFCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || isProd,
	})
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const (
	maxVotesPerMinute = 30
)

var voteLimiter = NewRateLimiter(maxVotesPerMinute, time.Minute)

type rateLimiter struct {
	limit      int
	maxEntries int
	window     time.Duration
	mu         sync.Mutex
	clients    map[string]*rateClient
}

type rateClient struct {
	count   int
	resetAt time.Time
}

func NewRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		limit:      limit,
		maxEntries: 100000,
		window:     window,
		clients:    make(map[string]*rateClient),
	}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) cleanup() {
	for {
		time.Sleep(rl.window)
		rl.mu.Lock()
		now := time.Now()
		for ip, client := range rl.clients {
			if now.After(client.resetAt) {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) Allow(key string) bool {
	if key == "" {
		return false
	}

	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	client, ok := rl.clients[key]
	if !ok || now.After(client.resetAt) {
		if len(rl.clients) >= rl.maxEntries {
			return false
		}
		rl.clients[key] = &rateClient{count: 1, resetAt: now.Add(rl.window)}
		return true
	}

	client.count++
	return client.count <= rl.limit
}

func clientIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

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
	crand "crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"log/slog"
	mathrand "math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/exploded/monitor/pkg/logship"
)

var templates *template.Template

func main() {

	var datasourcename string

	ship := logship.New(logship.Options{
		Endpoint: "https://monitor.mchugh.au/api/logs",
		APIKey:   os.Getenv("MONITOR_API_KEY"),
		App:      "alien",
		Level:    slog.LevelWarn,
	})
	defer ship.Shutdown()

	logger := slog.New(logship.Multi(
		slog.NewTextHandler(os.Stderr, nil),
		ship,
	))
	slog.SetDefault(logger)

	if datasourcename = os.Getenv("DATASOURCE"); datasourcename == "" {
		slog.Error("DATASOURCE env var is required")
		os.Exit(1)
	}

	InitDB(datasourcename)

	path, err := os.Getwd()
	if err != nil {
		slog.Error("failed to get working directory", "err", err)
		os.Exit(1)
	}

	templates = template.Must(template.ParseGlob(filepath.Join(path, "templates", "*.html")))

	slog.Info("starting", "path", path, "port", 8787)
	http.HandleFunc("/", siteroot)
	http.HandleFunc("/about", about)
	http.HandleFunc("/intro", intro)
	http.HandleFunc("/robots.txt", robots)
	http.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir(path+"/css"))))
	http.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(path+"/images"))))
	http.HandleFunc("/favicon.png", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(path, "images", "favicon.png"))
	})
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(path+"/static"))))
	server := &http.Server{
		Addr:         ":8787",
		Handler:      nil,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func about(w http.ResponseWriter, r *http.Request) {

	type AboutParams struct {
		Responses int64
		Question  string
	}

	var P AboutParams

	err := db.QueryRowContext(r.Context(), "SELECT count(*) FROM answer;").Scan(&P.Responses)
	if err != nil {
		slog.Error("about: query failed", "err", err)
	}

	w.Header().Set("cache-control", "private, max-age=0, no-cache")
	if err := templates.ExecuteTemplate(w, "about.html", P); err != nil {
		slog.Error("about: template failed", "err", err)
	}
}

func intro(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("cache-control", "public, max-age=3600")
	if err := templates.ExecuteTemplate(w, "intro.html", nil); err != nil {
		slog.Error("intro: template failed", "err", err)
	}
}

func robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("cache-control", "public, max-age=3600")
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
			P.Id = mathrand.Int63n(58) + 1
		} else {
			P.Id = idold
		}
	case http.MethodPost:
		if !voteLimiter.Allow(clientIP(r.RemoteAddr)) {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
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

		answerBit := 0
		if answer == "yes" {
			answerBit = 1
			_, err = db.ExecContext(r.Context(), "UPDATE question SET yes = yes + 1 WHERE id=?", idold)
		} else {
			_, err = db.ExecContext(r.Context(), "UPDATE question SET no = no + 1 WHERE id=?", idold)
		}
		if err != nil {
			slog.Error("vote update failed", "err", err, "question", idold)
		}

		_, err = db.ExecContext(r.Context(), "INSERT INTO answer (question, answer, submitter, submitteragent) VALUES(?, ?, ?, ?);", idold, answerBit, r.RemoteAddr, r.Header.Get("User-Agent"))
		if err != nil {
			slog.Error("vote insert failed", "err", err, "question", idold)
		}

		var Yes, No int64

		err = db.QueryRowContext(r.Context(), "SELECT short, yes, no FROM question WHERE id = ?", idold).Scan(&P.Previous, &Yes, &No)
		if err != nil {
			slog.Error("select stats failed", "err", err, "question", idold)
		}

		if Yes+No > 0 {
			P.PreviousValue = int64(100. * (float64(Yes) / float64(Yes+No)))
		} else {
			P.PreviousValue = 0
		}
		P.PreviousStats = "Yes:" + strconv.FormatInt(Yes, 10) + " No:" + strconv.FormatInt(No, 10) + " Total votes:" + strconv.FormatInt(Yes+No, 10)

		P.Id = idold + 1
		if P.Id == 60 {
			P.Id = 1
		}
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err = db.QueryRowContext(r.Context(), "SELECT category, question, picture, short FROM question WHERE id = ?", P.Id).Scan(&P.Category, &P.Question, &P.Picture, &P.Short)
	if err != nil {
		slog.Error("select question failed", "err", err, "question", P.Id)
		http.Error(w, "Unable to load question", http.StatusInternalServerError)
		return
	}

	w.Header().Set("cache-control", "private, max-age=0, no-cache")

	if err := templates.ExecuteTemplate(w, "index.html", P); err != nil {
		slog.Error("index: template failed", "err", err)
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
		Secure:   r.TLS != nil,
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
	limit   int
	window  time.Duration
	mu      sync.Mutex
	clients map[string]*rateClient
}

type rateClient struct {
	count   int
	resetAt time.Time
}

func NewRateLimiter(limit int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]*rateClient),
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
		return true
	}

	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	client, ok := rl.clients[key]
	if !ok || now.After(client.resetAt) {
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

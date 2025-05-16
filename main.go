package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/html"
	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"
)

var (
	port        = "8080"
	db          *sql.DB
	sessionStmt *sql.Stmt
	tmpl        *template.Template
	pageTmpl    map[string]*template.Template
	minifier    *minify.M
)

func main() {
	if v, ok := os.LookupEnv("LOG_FILE"); ok {
		logFile, err := os.OpenFile(v, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer logFile.Close()

		log.SetOutput(logFile)
	}

	if v, ok := os.LookupEnv("PORT"); ok {
		port = v
	}

	var err error
	if db, err = sql.Open("sqlite", "./db"); err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.Exec("PRAGMA foreign_keys = ON")
	db.Exec("PRAGMA journal_mode = WAL")
	db.Exec("PRAGMA busy_timeout = 10000")

	sessionStmt, err = db.Prepare(`
		SELECT
		users.username,
		email
		FROM user_log_in_sessions
		LEFT JOIN users ON user_log_in_sessions.username = users.username
		WHERE id = ? 
		LIMIT 1
	`)
	defer sessionStmt.Close()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			<-ticker.C
			cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
			if _, err := db.Exec("DELETE FROM user_log_in_sessions WHERE created_at < ?", cutoff); err != nil {
				log.Printf("delete user log in sessions failed: %v\n", err)
				continue
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			<-ticker.C
			cutoff := time.Now().UTC().Add(-10 * time.Minute)
			if _, err := db.Exec("DELETE FROM user_sign_up_email_tokens WHERE created_at < ?", cutoff); err != nil {
				log.Printf("delete user sign up tokens failed: %v\n", err)
				continue
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			<-ticker.C
			cutoff := time.Now().UTC().Add(-10 * time.Minute)
			if _, err := db.Exec("DELETE FROM user_log_in_email_tokens WHERE created_at < ?", cutoff); err != nil {
				log.Printf("delete user log in tokens failed: %v\n", err)
				continue
			}
		}
	}()

	tmpl = template.Must(template.New("base").Funcs(sprig.FuncMap()).ParseGlob("./template/*.tmpl"))
	files, err := os.ReadDir("./page")
	if err != nil {
		log.Fatal(err)
	}

	pageTmpl = map[string]*template.Template{}
	for _, file := range files {
		filename := file.Name()
		pageTmpl[filename] = template.Must(template.Must(tmpl.Clone()).ParseFiles("./page/" + filename))
	}

	minifier = minify.New()
	minifier.AddFunc("text/html", html.Minify)

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		u, _, err := getSessionUser(r)
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		executePage(w, r, "index.tmpl", map[string]any{
			"user": u,
		})
	})

	http.HandleFunc("GET /earthquake-master/{$}", func(w http.ResponseWriter, r *http.Request) {
		executePage(w, r, "earthquake-master.tmpl", nil)
	})

	http.HandleFunc("GET /lau-lang/{$}", func(w http.ResponseWriter, r *http.Request) {
		executePage(w, r, "lau-lang.tmpl", nil)
	})

	http.HandleFunc("GET /sign-up/{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok, err := getSessionUser(r); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if ok {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		executePage(w, r, "sign-up.tmpl", nil)
	})

	http.HandleFunc("POST /sign-up-by-email/{$}", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		username := r.FormValue("username")
		email := r.FormValue("email")

		// add validation
		rows, err := db.Query(`
			SELECT username, email
			FROM users
			WHERE username = ? OR email = ?
			`, username, email)
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		conflictFields := []string{}
		for rows.Next() {
			var u, e string
			rows.Scan(&u, &e)
			if u == username {
				conflictFields = append(conflictFields, "username")
			}
			if e == email {
				conflictFields = append(conflictFields, "email")
			}
		}
		if len(conflictFields) != 0 {
			http.Error(w, strings.Join(conflictFields, ","), http.StatusConflict)
			return
		}

		sixDigits := rand.Intn(900000) + 100000
		token := strconv.FormatInt(int64(sixDigits), 10)
		if _, err := db.Exec(`
			INSERT INTO user_sign_up_email_tokens (username, email, token) 
			VALUES (?, ?, ?) 
			ON CONFLICT DO UPDATE SET token = ?
			`, username, email, token, token); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		title := "信箱驗證碼"
		from := "台島 <no-reply@xn--kprw3s.tw>"
		var htmlBuffer bytes.Buffer
		if err = tmpl.ExecuteTemplate(&htmlBuffer, "sign-up-email", token); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		body := htmlBuffer.String()

		client := ses.NewFromConfig(cfg)
		client.SendEmail(ctx, &ses.SendEmailInput{
			Destination: &types.Destination{
				ToAddresses: []string{email},
			},
			Message: &types.Message{
				Subject: &types.Content{
					Data: &title,
				},
				Body: &types.Body{
					Html: &types.Content{
						Data: &body,
					},
				},
			},
			Source: &from,
		})

		w.Header().Add("HX-Redirect", fmt.Sprintf("/verify-sign-up-email/?username=%s&email=%s", url.QueryEscape(username), url.QueryEscape(email)))
		w.WriteHeader(http.StatusSeeOther)
	})

	http.HandleFunc("GET /verify-sign-up-email/{$}", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		username := query.Get("username")
		email := query.Get("email")
		if email == "" || username == "" {
			http.NotFound(w, r)
			return
		}
		executePage(w, r, "verify-sign-up-email.tmpl", map[string]any{
			"username": username,
			"email":    email,
		})
	})

	http.HandleFunc("POST /verify-sign-up-email/{$}", rateLimit(1, 10, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		username := r.FormValue("username")
		email := r.FormValue("email")
		token := r.FormValue("token")

		var count int
		if db.QueryRow(`
			SELECT COUNT() FROM user_sign_up_email_tokens
			WHERE username = ?
			AND email = ?
			AND token = ?
		`, username, email, token).Scan(&count) == sql.ErrNoRows || count == 0 {
			http.NotFound(w, r)
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		if res, err := tx.Exec("INSERT INTO users (username, email) VALUES (?, ?)", username, email); err != nil {
			tx.Rollback()
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if affected, err := res.RowsAffected(); err != nil {
			tx.Rollback()
			log.Println("res.RowsAffected failed in verify-sign-up-email")
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if affected == 0 {
			tx.Rollback()
			http.Error(w, http.StatusText(http.StatusConflict), http.StatusConflict)
			return
		}
		if _, err := tx.Exec("DELETE FROM user_sign_up_email_tokens WHERE username = ? AND email = ?", username, email); err != nil {
			tx.Rollback()
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if err := tx.Commit(); err != nil {
			tx.Rollback()
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		bs := make([]byte, 32)
		if _, err = crand.Read(bs); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		sessionId := base64.URLEncoding.EncodeToString(bs)

		if _, err := db.Exec(`
			INSERT INTO user_log_in_sessions (id, username) 
			VALUES (?, ?)
		`, sessionId, username); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		cookie := http.Cookie{
			Name:  "session",
			Value: sessionId,
			Path:  "/", Expires: time.Now().Add(7 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   true,
		}

		http.SetCookie(w, &cookie)
		w.Header().Add("HX-Redirect", "/")
		w.WriteHeader(http.StatusSeeOther)
	}))

	http.HandleFunc("GET /log-in/{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok, err := getSessionUser(r); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if ok {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		executePage(w, r, "log-in.tmpl", nil)
	})

	http.HandleFunc("GET /log-in-by-email/{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok, err := getSessionUser(r); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if ok {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		executePage(w, r, "log-in-by-email.tmpl", nil)
	})

	http.HandleFunc("POST /log-in-by-email/{$}", rateLimit(1, 10, func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		email := r.FormValue("email")

		var username string
		if err := db.QueryRow("SELECT username FROM users WHERE email = ?", email).Scan(&username); errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		sixDigits := rand.Intn(900000) + 100000
		token := strconv.FormatInt(int64(sixDigits), 10)
		if _, err := db.Exec(`
			INSERT INTO user_log_in_email_tokens (email, token) 
			VALUES (?, ?)
		`, email, token); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		ctx := r.Context()
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		title := "信箱驗證碼"
		from := "台島 <no-reply@xn--kprw3s.tw>"
		var htmlBuffer bytes.Buffer
		if err = tmpl.ExecuteTemplate(&htmlBuffer, "sign-up-email", token); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		body := htmlBuffer.String()

		client := ses.NewFromConfig(cfg)
		client.SendEmail(ctx, &ses.SendEmailInput{
			Destination: &types.Destination{
				ToAddresses: []string{email},
			},
			Message: &types.Message{
				Subject: &types.Content{
					Data: &title,
				},
				Body: &types.Body{
					Html: &types.Content{
						Data: &body,
					},
				},
			},
			Source: &from,
		})

		w.Header().Add("HX-Redirect", fmt.Sprintf("/verify-log-in-email/?email=%s", url.QueryEscape(email)))
		w.WriteHeader(http.StatusSeeOther)
	}))

	http.HandleFunc("GET /verify-log-in-email/{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok, err := getSessionUser(r); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if ok {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		query := r.URL.Query()
		email := query.Get("email")
		if email == "" {
			http.NotFound(w, r)
			return
		}

		executePage(w, r, "verify-log-in-email.tmpl", map[string]any{
			"email": email,
		})
	})

	http.HandleFunc("POST /verify-log-in-email/{$}", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		email := r.FormValue("email")
		token := r.FormValue("token")

		var username string
		if db.QueryRow(`
			SELECT username 
			FROM user_log_in_email_tokens
			LEFT JOIN users ON user_log_in_email_tokens.email = users.email
			WHERE user_log_in_email_tokens.email = ?
			AND token = ?
		`, email, token).Scan(&username) == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}

		if _, err := db.Exec("DELETE FROM user_log_in_email_tokens WHERE email = ? AND token = ?", email, token); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		bs := make([]byte, 32)
		if _, err = crand.Read(bs); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		sessionId := base64.URLEncoding.EncodeToString(bs)

		if _, err := db.Exec(`
			INSERT INTO user_log_in_sessions (id, username) 
			VALUES (?, ?)
		`, sessionId, username); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		cookie := http.Cookie{
			Name:  "session",
			Value: sessionId,
			Path:  "/", Expires: time.Now().Add(7 * 24 * time.Hour),
			HttpOnly: true,
			Secure:   true,
		}

		http.SetCookie(w, &cookie)
		w.Header().Add("HX-Redirect", "/")
		w.WriteHeader(http.StatusSeeOther)
	})

	http.HandleFunc("POST /log-out/{$}", func(w http.ResponseWriter, r *http.Request) {
		u, ok, err := getSessionUser(r)
		if err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		} else if ok {
			db.Exec("DELETE FROM user_log_in_sessions WHERE username = ?", u.Username)
		}

		w.Header().Add("HX-Redirect", "/")
		w.WriteHeader(http.StatusSeeOther)
	})

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServerFS(os.DirFS("static"))))

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func executePage(w http.ResponseWriter, r *http.Request, name string, data any) {
	var writer io.Writer = w

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		gzipWriter := gzip.NewWriter(w)
		defer gzipWriter.Close()

		writer = gzipWriter
	}

	minifyWriter := minifier.Writer("text/html", writer)
	defer minifyWriter.Close()

	page := pageTmpl[name]
	if err := page.ExecuteTemplate(minifyWriter, "page", data); err != nil {
		log.Println(err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
}

func executeTemplates(w http.ResponseWriter, data any, trigger string, names ...string) {
	minifyWriter := minifier.Writer("text/html", w)
	defer minifyWriter.Close()

	if len(trigger) > 0 {
		w.Header().Add("HX-Trigger", trigger)
	}

	for _, name := range names {
		if err := tmpl.ExecuteTemplate(minifyWriter, name, data); err != nil {
			log.Println(err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}
}

type User struct {
	Email    string
	Username string
}

func getSessionUser(r *http.Request) (*User, bool, error) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, false, nil
	}

	rows, err := sessionStmt.Query(cookie.Value)
	if err != nil {
		return nil, false, err
	}

	u := User{}
	if !rows.Next() {
		return nil, false, nil
	}

	if err := rows.Scan(&u.Username, &u.Email); err != nil {
		return nil, false, err
	}

	return &u, true, nil
}

func rateLimit(limit float64, burst int, next http.HandlerFunc) http.HandlerFunc {
	limiter := rate.NewLimiter(rate.Limit(limit), burst)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

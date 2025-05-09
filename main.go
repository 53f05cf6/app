package main

import (
	"bytes"
	"compress/gzip"
	crand "crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
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
	port = "8080"

	db       *sql.DB
	tmpl     *template.Template
	pageTmpl map[string]*template.Template
	minifier *minify.M
)

func main() {
	if v, ok := os.LookupEnv("LOG_FILE"); ok {
		logFile, err := os.OpenFile(v, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatal(err)
		}
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

	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()

		for {
			<-ticker.C
			cutoff := time.Now().Add(-7 * 24 * time.Hour).Unix()
			if _, err := db.Exec("DELETE FROM user_log_in_sessions WHERE created_at < ?", cutoff); err != nil {
				log.Printf("deleting user sessions failed: %v\n", err)
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
		pageTmpl[filename] = template.Must(template.Must(tmpl.Clone()).ParseFiles(fmt.Sprintf("./page/%s", filename)))
	}

	minifier = minify.New()
	minifier.AddFunc("text/html", html.Minify)

	// TTL for user sign-up token 5m

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		executePage(w, r, "index.tmpl", nil)
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
			w.WriteHeader(http.StatusInternalServerError)
			return
		} else if ok {
			w.Header().Add("location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}

		executePage(w, r, "sign-up.tmpl", nil)
	})

	http.HandleFunc("POST /sign-up-by-email/{$}", func(w http.ResponseWriter, r *http.Request) {
		username := r.FormValue("username")
		email := r.FormValue("email")
		// add validation

		rows, err := db.Query(`
			SELECT username, email FROM users
			WHERE username = ? OR email = ?
			`, username, email)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
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
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(strings.Join(conflictFields, ",")))
			return
		}

		sixDigits := rand.Intn(900000) + 100000
		token := fmt.Sprintf("%d", sixDigits)
		if _, err := db.Exec("INSERT INTO user_sign_up_email_tokens (username, email, token) VALUES (?, ?, ?) ON CONFLICT DO UPDATE SET token = ?", username, email, token, token); err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ctx := r.Context()
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Println(err)
		}

		from := "台島 <no-reply@xn--kprw3s.tw>"
		title := "信箱驗證碼"
		var htmlBuffer bytes.Buffer
		err = tmpl.ExecuteTemplate(&htmlBuffer, "sign-up-email", token)
		if err != nil {
			log.Panic(err)
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

		w.Header().Add("HX-Redirect", fmt.Sprintf("/settings/?username=%s&email=%s", username, email))
		w.WriteHeader(http.StatusSeeOther)
	})

	http.HandleFunc("GET /verify-email/{$}", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		username := query.Get("username")
		email := query.Get("email")
		if email == "" || username == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		executePage(w, r, "verify-email.tmpl", map[string]any{
			"username": username,
			"email":    email,
		})
	})

	http.HandleFunc("POST /verify-email/{$}", rateLimit(func(w http.ResponseWriter, r *http.Request) {
		username := r.FormValue("username")
		email := r.FormValue("email")
		token := r.FormValue("token")
		row := db.QueryRow(`
			SELECT * FROM user_sign_up_email_tokens
			WHERE username = ?
			AND email = ?
			AND token = ?
		`, username, email, token)
		if row.Scan() == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if res, err := db.Exec("INSERT INTO users (username, email) VALUES (?, ?)", username, email); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		} else if affected, err := res.RowsAffected(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		} else if affected == 0 {
			w.WriteHeader(http.StatusConflict)
			return
		}

		bs := make([]byte, 32)
		if _, err = crand.Read(bs); err != nil {
			log.Panic(err)
		}
		sessionId := base64.URLEncoding.EncodeToString(bs)

		if _, err := db.Exec("INSERT INTO user_log_in_sessions (id, username) VALUES (?, ?)", sessionId, username); err != nil {
			log.Panic(err)
		}

		cookie := http.Cookie{Name: "session", Value: token, Path: "/", Expires: time.Now().Add(7 * 24 * time.Hour)}

		http.SetCookie(w, &cookie)
		w.Header().Add("HX-Redirect", "/")
		w.WriteHeader(http.StatusSeeOther)
	}))

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("assets"))))

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
		w.WriteHeader(http.StatusInternalServerError)
		log.Panic(err)
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
			w.WriteHeader(http.StatusInternalServerError)
			log.Panic(err)
		}
	}
}

func getSessionUser(r *http.Request) (*User, bool, error) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, false, nil
	}

	var user *User
	rows, err := db.Query(`
		SELECT
		email
		username,
		FROM users
		WHERE user_log_in_sessions.id = ? 
		LIMIT 1`, cookie.Value)

	if err != nil {
		return nil, false, err
	}

	u := User{}
	if rows.Next() {
		rows.Scan(u.Email, u.Username)
	} else {
		return nil, false, nil
	}

	return user, true, nil
}

func rateLimit(next http.HandlerFunc) http.HandlerFunc {
	limiter := rate.NewLimiter(1, 10)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

type User struct {
	Email    string
	Username string
}

package main

import (
	"compress/gzip"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Masterminds/sprig/v3"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2"
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
			w.WriteHeader(http.StatusInternalServerError)
			log.Panic(err)
		} else if ok {
			w.Header().Add("location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}

		executePage(w, r, "sign-up.tmpl", nil)
	})

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
		WHERE user_sessions.id = ? 
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

type User struct {
	Email    string
	Username string
}

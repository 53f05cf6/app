package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	if os.Getenv("PORT") == "" {
		os.Setenv("PORT", "8080")
	}

	db, err := sql.Open("sqlite3", "ti.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.Query("SELECT id, title, content, created_at FROM news ORDER BY id DESC")
		if err != nil {
			log.Fatal(err)
		}
		defer rows.Close()

		var news []News
		for rows.Next() {
			var n News
			err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.CreatedAt)
			if err != nil {
				log.Fatal(err)
			}
			news = append(news, n)
		}

		tmpl, err := template.ParseFiles("index.html")
		if err != nil {
			log.Fatal(err)
		}
		err = tmpl.Execute(w, news)
		if err != nil {
			log.Fatal(err)
		}
	})

	http.HandleFunc("/news/{$}", func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if err != nil {
			log.Println("http.Request.ParseForm failed")
			w.WriteHeader(400)
			return
		}

		switch r.Method {
		case http.MethodPost:
			_, err := db.Exec("INSERT INTO news (title, content, updated_at) VALUES (?, ?, datetime())", r.Form.Get("title"), r.Form.Get("content"))
			if err != nil {
				log.Printf("db.Exec failed: %v", err)
				return
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	})

	http.HandleFunc("/news/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			err := r.ParseForm()
			if err != nil {
				log.Println("http.Request.ParseForm failed")
				w.WriteHeader(400)
				return
			}

			id := r.PathValue("id")

			_, err = db.Exec("UPDATE news SET title=?, content=?, updated_at=datetime() WHERE id = ?", r.Form.Get("title"), r.Form.Get("content"), id)
			if err != nil {
				log.Printf("db.Exec failed: %v", err)
				return
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}

	})

	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}

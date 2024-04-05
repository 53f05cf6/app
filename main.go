package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "ti.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
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

		// print result
		err = tmpl.Execute(w, news)
		if err != nil {
			log.Fatal(err)
		}
	})

	http.ListenAndServe(":8080", nil)
}

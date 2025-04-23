package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
)

var (
	port = "8080"

	tmpl     *template.Template
	pageTmpl map[string]*template.Template
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

	index := template.Must(template.ParseFiles("index.html"))
	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		index.Execute(w, nil)
		return
	})

	earthquake := template.Must(template.ParseFiles("earthquake.html"))
	http.HandleFunc("GET /earthquake/{$}", func(w http.ResponseWriter, r *http.Request) {

		earthquake.Execute(w, nil)
	})

	laulang := template.Must(template.ParseFiles("laulang.html"))
	http.HandleFunc("GET /laulang/{$}", func(w http.ResponseWriter, r *http.Request) {

		laulang.Execute(w, nil)
	})

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

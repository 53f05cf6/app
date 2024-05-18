package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mmcdole/gofeed"
	"github.com/sashabaranov/go-openai"
	"github.com/twilio/twilio-go"
	verify "github.com/twilio/twilio-go/rest/verify/v2"
)

var (
	loc, _          = time.LoadLocation("Asia/Taipei")
	port            = "8080"
	openAiToken     string
	assistantId     string
	cwaToken        string
	twilioServiceId string
	logFile         string
	cfg             openai.ClientConfig
	sources         []Source
	feedChans       = map[string](chan string){}
)

type User struct {
	Email     string
	Name      string
	Prompt    string
	Sources   map[string]bool
	Feed      template.HTML
	Template  string
	Subscribe bool
}

type SourceType string

const (
	SourceTypeRSS SourceType = "RSS"
)

type Source struct {
	Type SourceType `json:"type"`
	Name string     `json:"name"`
	Site string     `json:"site"`
	Feed string     `json:"feed"`
}

func main() {
	if p, ok := os.LookupEnv("PORT"); ok {
		port = p
	}

	if lf, ok := os.LookupEnv("LOG_FILE"); ok {
		logFile, err := os.OpenFile(lf, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatal(err)
		}
		log.SetOutput(logFile)
	}

	if t, ok := os.LookupEnv("OPENAI_TOKEN"); !ok {
		log.Fatal("OpenAI token missing")
	} else {
		openAiToken = t
		cfg = openai.DefaultConfig(t)
		cfg.AssistantVersion = "v2"
	}

	if aId, ok := os.LookupEnv("OPENAI_ASSISTANT_ID"); !ok {
		log.Fatal("OpenAI assistant id missing")
	} else {
		assistantId = aId
	}

	if t, ok := os.LookupEnv("CWA_TOKEN"); !ok {
		log.Fatal("CWA token missing")
	} else {
		cwaToken = t
	}

	if _, ok := os.LookupEnv("TWILIO_AUTH_TOKEN"); !ok {
		log.Fatal("Twilio auth token missing")
	}

	if _, ok := os.LookupEnv("TWILIO_ACCOUNT_SID"); !ok {
		log.Fatal("Twilio account sid missing")
	}

	if t, ok := os.LookupEnv("TWILIO_SERVICE_ID"); !ok {
		log.Fatal("Twilio service id missing")
	} else {
		twilioServiceId = t
	}

	if f, err := os.ReadFile("sources.json"); err != nil {
		log.Fatal("cannot read sources file")
	} else if err := json.Unmarshal(f, &sources); err != nil {
		log.Fatal("cannot unmarshal sources file")
	}

	go func() {
		for {
			db := openDB()
			t := time.Now().Add(-7 * 24 * time.Hour)
			if _, err := db.Exec("DELETE FROM sessions WHERE created_at < ?", t.Format(time.DateTime)); err != nil {
				time.Sleep(time.Minute)
				db.Close()
				continue
			}
			db.Close()

			now := time.Now()
			time.Sleep(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Add(24 * time.Hour).Sub(now))
		}
	}()

	http.HandleFunc("GET /styles.css/{$}", func(w http.ResponseWriter, r *http.Request) {
		asset, err := os.ReadFile("assets/styles.css")
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "text/css")
		if _, err := w.Write(asset); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /htmx-sse.js/{$}", func(w http.ResponseWriter, r *http.Request) {
		asset, err := os.ReadFile("htmx-sse.js")
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "text/javascript")
		if _, err := w.Write(asset); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /htmx.js/{$}", func(w http.ResponseWriter, r *http.Request) {
		asset, err := os.ReadFile("htmx.js")
		if err != nil {
			log.Fatal(err)
		}

		w.Header().Add("Content-Type", "text/javascript")
		if _, err := w.Write(asset); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/layout.html", "./template/index.html")
		if err != nil {
			log.Fatal(err)
		}

		m := map[string]any{
			"sources": sources,
		}

		if user, err := getSessionUser(r); err == nil {
			log.Printf("visit: %s\n", user.Email)
			m["user"] = user
		}

		if err = tmpl.Execute(w, m); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("POST /template/search/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/search.html")
		if err != nil {
			log.Fatal(err)
		}

		prompt := r.FormValue("prompt")

		userSources := []string{}
		for _, source := range sources {
			on := r.FormValue(source.Name)
			if on == "" {
				continue
			}

			userSources = append(userSources, source.Name)
		}
		sourcesStr := strings.Join(userSources, ",")

		m := map[string]string{
			"prompt":  prompt,
			"sources": sourcesStr,
		}

		if err = tmpl.Execute(w, m); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /search/{$}", func(w http.ResponseWriter, r *http.Request) {
		user, err := getSessionUser(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		query := r.URL.Query()
		prompt := query.Get("prompt")
		sourcesStr := query.Get("sources")
		userSources := strings.Split(sourcesStr, ",")

		db := openDB()
		defer db.Close()
		if _, err := db.Exec("UPDATE users SET sources = ?, prompt = ? WHERE email = ?", sourcesStr, prompt, user.Email); err != nil {
			log.Panic(err)
		}

		m := map[string]struct{}{}
		for _, source := range userSources {
			m[source] = struct{}{}
		}

		now := time.Now()
		csv := "標題,說明,時間,連結URL,圖片URL\n"
		for _, source := range sources {
			if _, ok := m[source.Name]; !ok {
				continue
			}

			fp := gofeed.NewParser()
			feed, _ := fp.ParseURL(source.Feed)
			for _, item := range feed.Items {
				if now.Sub(*item.PublishedParsed) > 24*time.Hour {
					continue
				}

				imgUrl := "n/a"
				if item.Image != nil {
					imgUrl = item.Image.URL
				}

				csv += fmt.Sprintf("%s,%s,%s,%s,%s\n", item.Title, item.Description, item.PublishedParsed.Format(time.DateTime), item.Link, imgUrl)

			}

		}

		systemPrompt := fmt.Sprintf(`
你的目標是幫助用戶了解台灣發生的新聞。
只利用prompt所知道的知識回答用戶想要知道的內容。
遵守以下規則:
1.輸出必須是html
2.切勿使用codeblock
3.勿使用<html><head><body>tags
4.根據以下的知識回答
5.只輸出跟用戶需求相關的內容
---
現在時間: %s
知識: %s`, now.Format(time.DateTime), csv)
		client := openai.NewClientWithConfig(cfg)
		stream, err := client.CreateChatCompletionStream(r.Context(), openai.ChatCompletionRequest{
			Model: openai.GPT4o,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: systemPrompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: fmt.Sprintf("我想知道: %s", prompt),
				},
			},
		})
		defer stream.Close()
		if err != nil {
			log.Panic(err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		msg := ""
		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				fmt.Fprintf(w, "event: end\ndata: \n\n")

				if _, err := db.Exec("UPDATE users SET feed = ? WHERE email = ?", msg, user.Email); err != nil {
					log.Panic(err)
				}
				log.Println("---")
				log.Println(user.Email)
				log.Println(prompt)
				log.Println(msg)
				log.Println("---")
				break
			}

			if err != nil {
				log.Panic(err)
				return
			}

			msg += strings.ReplaceAll(response.Choices[0].Delta.Content, "\n", "")
			fmt.Fprintf(w, "data: %s\n\n", msg)
		}
	})

	http.HandleFunc("GET /help/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/layout.html", "./template/help.html")
		if err != nil {
			log.Fatal(err)
		}

		user, err := getSessionUser(r)

		if err = tmpl.Execute(w, map[string]any{"user": user}); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /login/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/layout.html", "./template/login.html")
		if err != nil {
			log.Fatal(err)
		}

		user, err := getSessionUser(r)
		if err == nil {
			w.Header().Add("location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}

		if err = tmpl.Execute(w, map[string]any{"user": user}); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("POST /login/{$}", func(w http.ResponseWriter, r *http.Request) {
		email := r.FormValue("email")

		params := &verify.CreateVerificationParams{}
		params.SetTo(email)
		params.SetChannel("email")

		client := twilio.NewRestClient()
		if _, err := client.VerifyV2.CreateVerification(twilioServiceId, params); err != nil {
			log.Panic(err)
		}

		w.Header().Add("HX-Redirect", fmt.Sprintf("/verify/?email=%s", email))
	})

	http.HandleFunc("GET /verify/{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := getSessionUser(r); err == nil {
			w.Header().Add("location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}

		tmpl, err := template.ParseFiles("./template/layout.html", "./template/verify.html")
		if err != nil {
			log.Fatal(err)
		}

		query := r.URL.Query()
		m := map[string]any{
			"email": query.Get("email"),
		}

		if err = tmpl.Execute(w, m); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("POST /verify/{$}", func(w http.ResponseWriter, r *http.Request) {
		email := r.URL.Query().Get("email")

		params := &verify.CreateVerificationCheckParams{}
		params.SetTo(email)
		params.SetCode(r.FormValue("code"))

		client := twilio.NewRestClient()
		resp, err := client.VerifyV2.CreateVerificationCheck(twilioServiceId, params)
		if err != nil {
			log.Panic(err)
		}

		if *resp.Status != "approved" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		db := openDB()
		defer db.Close()

		defaultName := strings.Split(email, "@")[0]
		if _, err := db.Exec("INSERT INTO users (email, name, prompt, sources, feed) VALUES (?, ?, ?, ?, ?) ON CONFLICT(email) DO NOTHING", email, defaultName, `台灣今天有什麼重大新聞?
今天出門該怎麼穿？`, "報導者,公視新聞,ETtoday,自由時報", ""); err != nil {
			log.Panic(err)
		}

		bytes := make([]byte, 32)
		if _, err = rand.Read(bytes); err != nil {
			log.Panic(err)
		}
		id := base64.URLEncoding.EncodeToString(bytes)

		if _, err := db.Exec("INSERT INTO sessions (id, email) VALUES (?, ?)", id, email); err != nil {
			log.Panic(err)
		}

		cookie := http.Cookie{Name: "session", Value: id, Path: "/"}
		http.SetCookie(w, &cookie)
	})

	http.HandleFunc("GET /setting/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/layout.html", "./template/setting.html")
		if err != nil {
			log.Fatal(err)
		}

		user, err := getSessionUser(r)
		if err != nil {
			w.Header().Add("location", "/")
			w.WriteHeader(http.StatusFound)
			return
		}

		if err = tmpl.Execute(w, map[string]any{"user": user}); err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("PUT /setting/{$}", func(w http.ResponseWriter, r *http.Request) {
		user, err := getSessionUser(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		name := r.FormValue("name")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		subscribe := false
		if r.FormValue("subscribe") == "on" {
			subscribe = true
		}

		db := openDB()
		defer db.Close()
		if _, err := db.Exec("UPDATE users SET name = ?, subscribe  = ? WHERE email = ?", name, subscribe, user.Email); err != nil {
			log.Panic(err)
		}
	})

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func openDB() *sql.DB {
	db, err := sql.Open("sqlite3", "./db")
	if err != nil {
		log.Panic(err)
	}

	return db
}

var (
	ErrUserNotLoggedIn = errors.New("user not logged in")
)

func getSessionUser(r *http.Request) (*User, error) {
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil, ErrUserNotLoggedIn
	}

	db := openDB()
	defer db.Close()

	user := User{}
	sourcesStr := ""
	if err := db.QueryRow("SELECT sessions.email, users.name, users.prompt, users.sources, users.feed, users.template, users.subscribe FROM sessions JOIN users ON sessions.email = users.email WHERE sessions.id = ?", cookie.Value).Scan(&user.Email, &user.Name, &user.Prompt, &sourcesStr, &user.Feed, &user.Template, &user.Subscribe); err != nil {
		return nil, ErrUserNotLoggedIn
	}

	user.Sources = map[string]bool{}
	for _, s := range strings.Split(sourcesStr, ",") {
		user.Sources[s] = true
	}

	return &user, nil
}

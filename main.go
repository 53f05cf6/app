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
	"sync"
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
	Email   *string
	Name    *string
	Prompt  *string
	Sources map[string]bool
	Feed    template.HTML
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
			t := timeNow().Add(-7 * 24 * time.Hour)
			if _, err := db.Exec("DELETE FROM sessions WHERE created_at < ?", t.Format(time.DateTime)); err != nil {
				log.Println(err)
				time.Sleep(time.Minute)
				db.Close()
				continue
			}
			db.Close()

			now := timeNow()
			time.Sleep(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).Add(24 * time.Hour).Sub(now))
		}
	}()

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		m := map[string]any{
			"sources": sources,
		}

		email, err := getLoggedInUserEmail(r)
		if err == nil {
			user := &User{
				Email: &email,
			}

			db := openDB()
			var sourcesStr *string
			var feedStr *string
			if err := db.QueryRow("SELECT name, prompt, sources, feed FROM users WHERE email = ?", email).Scan(&user.Name, &user.Prompt, &sourcesStr, &feedStr); err == sql.ErrNoRows {
				log.Printf("user not found: %s", email)
				user = nil
			} else if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			} else {
				if sourcesStr != nil {
					userSources := strings.Split(*sourcesStr, ",")
					user.Sources = map[string]bool{}
					for _, s := range userSources {
						user.Sources[s] = true
					}
				}

				user.Feed = template.HTML(*feedStr)
				m["user"] = user
			}

		}

		tmpl, err := template.ParseFiles("./template/layout.html", "./template/index.html")
		if err != nil {
			log.Fatal(err)
		}

		err = tmpl.Execute(w, m)
		if err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /login/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/layout.html", "./template/login.html")
		if err != nil {
			log.Fatal(err)
		}

		err = tmpl.Execute(w, nil)
		if err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("POST /login/{$}", func(w http.ResponseWriter, r *http.Request) {
		email := r.FormValue("email")
		log.Printf("login: %s try to login", email)

		params := &verify.CreateVerificationParams{}
		params.SetTo(email)
		params.SetChannel("email")

		client := twilio.NewRestClient()
		_, err := client.VerifyV2.CreateVerification(twilioServiceId, params)
		if err != nil {
			log.Panic(err)
		}

		w.Header().Add("HX-Redirect", fmt.Sprintf("/verify/?email=%s", email))
	})

	http.HandleFunc("GET /verify/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/layout.html", "./template/verify.html")
		if err != nil {
			log.Fatal(err)
		}

		query := r.URL.Query()

		err = tmpl.Execute(w, query.Get("email"))
		if err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("POST /verify/{$}", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		code := r.FormValue("code")
		email := query.Get("email")
		log.Printf("verify: %s", email)

		params := &verify.CreateVerificationCheckParams{}
		params.SetTo(email)
		params.SetCode(code)

		client := twilio.NewRestClient()
		resp, err := client.VerifyV2.CreateVerificationCheck(twilioServiceId, params)
		if err != nil {
			log.Panic(err)
		}

		log.Println(*resp.Status)
		if *resp.Status != "approved" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		defaultName := strings.Split(email, "@")[0]

		db, err := sql.Open("sqlite3", "./db")
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()

		if _, err := db.Exec("INSERT INTO users (email, name, prompt, sources, feed) VALUES (?, ?, ?, ?, ?) ON CONFLICT(email) DO NOTHING", email, defaultName, `台灣今天有什麼重大新聞?
今天出門該怎麼穿？`, "報導者,公視新聞,ETtoday,自由時報", ""); err != nil {
			log.Panic(err)
		}

		bytes := make([]byte, 32)
		_, err = rand.Read(bytes)
		if err != nil {
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

		email, err := getLoggedInUserEmail(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			log.Println(err)
			return
		}

		db := openDB()
		name := ""
		if err := db.QueryRow("SELECT name FROM users WHERE email = ?", email).Scan(&name); err == sql.ErrNoRows {
			w.WriteHeader(http.StatusNotFound)
			return
		} else if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		m := map[string]string{
			"name":  name,
			"email": email,
		}

		err = tmpl.Execute(w, m)
		if err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("POST /ask/{$}", func(w http.ResponseWriter, r *http.Request) {
		email, err := getLoggedInUserEmail(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			log.Println(err)
			return
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

		db := openDB()
		if _, err := db.Exec("UPDATE users SET sources = ?, prompt = ? WHERE email = ?", sourcesStr, prompt, email); err != nil {
			log.Panic(err)
		}

		m := map[string]struct{}{}
		for _, source := range userSources {
			m[source] = struct{}{}
		}

		now := time.Now().In(loc)
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
你的目標是幫助用戶了解台灣發生的新聞
只利用prompt所知道的知識回答用戶想要知道的內容。
遵守以下規則:
1.輸出必須是台灣正體中文
2.輸出必須是html
3.切勿使用codeblock
4.勿使用<html><head><body>tags
5.如果沒有相關的知識則拒絕回答
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

		if feedChans[email] == nil {
			log.Panic("no feed channel found: " + email)
			return
		}

		ch := feedChans[email]
		msg := ""
		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				if _, err := db.Exec("UPDATE users SET feed = ? WHERE email = ?", msg, email); err != nil {
					log.Panic(err)
				}
				log.Println("---")
				log.Println(email)
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
			ch <- msg
		}

		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("GET /feed/{$}", func(w http.ResponseWriter, r *http.Request) {
		email, err := getLoggedInUserEmail(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			log.Println(err)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		var mux sync.Mutex
		ch := make(chan string)

		mux.Lock()
		feedChans[email] = ch
		mux.Unlock()

		for feed := range ch {
			fmt.Fprintf(w, "data: %s\n\n", feed)
		}

		delete(feedChans, email)
	})

	http.HandleFunc("PUT /setting/{$}", func(w http.ResponseWriter, r *http.Request) {
		email, err := getLoggedInUserEmail(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			log.Println(err)
			return
		}

		name := r.FormValue("name")
		if name == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		db := openDB()
		if _, err := db.Exec("UPDATE users SET name = ? WHERE email = ?", name, email); err != nil {
			log.Panic(err)
		}

		w.Header().Add("HX-Redirect", "/")
	})

	http.HandleFunc("GET /help/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./template/layout.html", "./template/help.html")
		if err != nil {
			log.Fatal(err)
		}

		err = tmpl.Execute(w, nil)
		if err != nil {
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

func getLoggedInUserEmail(r *http.Request) (string, error) {
	db := openDB()
	defer db.Close()

	cookie, err := r.Cookie("session")
	if err != nil {
		return "", ErrUserNotLoggedIn
	}

	email := ""
	if err := db.QueryRow("SELECT email FROM sessions WHERE id = ?", cookie.Value).Scan(&email); err != nil {
		return "", ErrUserNotLoggedIn
	}

	return email, nil
}

func timeNow() time.Time {
	return time.Now().In(loc)
}

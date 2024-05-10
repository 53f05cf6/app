package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/sashabaranov/go-openai"
	"github.com/twilio/twilio-go"
	verify "github.com/twilio/twilio-go/rest/verify/v2"

	"53f05cf6/source"
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
	sessions        = map[string]string{"test": "you@hsuyuting.com"}
)

type User struct {
	email string `field:""`
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

	dp := ""
	go func() {
		for {
			newDp, err := createDefaultPage()
			if err != nil {
				log.Println(err)
				time.Sleep(5 * time.Minute)
				continue
			}

			dp = newDp
			log.Println(dp)

			now := time.Now().In(loc)
			h := now.Hour()
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

			var sleepDuration time.Duration
			if h >= 0 && h < 8 {
				sleepDuration = today.Add(8 * time.Hour).Sub(now)
			} else if h >= 8 && h < 12 {
				sleepDuration = today.Add(12 * time.Hour).Sub(now)
			} else if h >= 12 && h < 16 {
				sleepDuration = today.Add(16 * time.Hour).Sub(now)
			} else if h >= 16 && h < 20 {
				sleepDuration = today.Add(20 * time.Hour).Sub(now)
			} else {
				sleepDuration = today.Add(32 * time.Hour).Sub(now)
			}

			log.Println(sleepDuration.String())
			time.Sleep(sleepDuration)
		}
	}()

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		email := ""
		name := ""
		if cookie, err := r.Cookie("session"); err == nil && sessions[cookie.Value] != "" {
			email = sessions[cookie.Value]

			db, err := sql.Open("sqlite3", "./db")
			if err != nil {
				log.Fatal(err)
			}
			defer db.Close()

			if err := db.QueryRow("SELECT name FROM users WHERE email = ?", email).Scan(&name); err == sql.ErrNoRows {
				w.WriteHeader(http.StatusNotFound)
				return
			} else if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		m := map[string]any{
			"dp":    template.HTML(dp),
			"name":  name,
			"email": email,
		}

		tmpl, err := template.ParseFiles("./component/layout.html", "./component/index.html")
		if err != nil {
			log.Fatal(err)
		}

		err = tmpl.Execute(w, m)
		if err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /component/{comp}/{$}", func(w http.ResponseWriter, r *http.Request) {
		compPath := fmt.Sprintf("./component/%s.html", r.PathValue("comp"))
		tmpl, err := template.ParseFiles(compPath)
		if err != nil {
			log.Fatal(err)
		}

		queries := r.URL.Query()

		err = tmpl.ExecuteTemplate(w, r.PathValue("comp"), queries.Get("prompt"))
		if err != nil {
			log.Panic(err)
		}
	})

	http.HandleFunc("GET /login/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./component/layout.html", "./component/login.html")
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
		tmpl, err := template.ParseFiles("./component/layout.html", "./component/verify.html")
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

		if _, err := db.Exec("INSERT INTO users (email, name) VALUES (?, ?) ON CONFLICT(email) DO NOTHING", email, defaultName); err != nil {
			log.Panic(err)
		}

		sessionId := createRandomString()
		sessions[sessionId] = email

		log.Println(sessions)

		cookie := http.Cookie{Name: "session", Value: sessionId, Path: "/"}
		http.SetCookie(w, &cookie)
	})

	http.HandleFunc("GET /setting/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./component/layout.html", "./component/setting.html")
		if err != nil {
			log.Fatal(err)
		}

		email := ""
		if cookie, err := r.Cookie("session"); err == nil {
			email = sessions[cookie.Value]
		}

		db, err := sql.Open("sqlite3", "./db")
		if err != nil {
			log.Fatal(err)
		}
		defer db.Close()

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

	http.HandleFunc("GET /help/{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("./component/layout.html", "./component/help.html")
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

func createDefaultPage() (string, error) {
	// if port == "8080" {
	// 	return "page", nil
	// }

	ctx := context.Background()
	weather := source.Forecast36Hours{Token: cwaToken}
	weather.Get()
	client := openai.NewClientWithConfig(cfg)
	run, err := client.CreateThreadAndRun(ctx, openai.CreateThreadAndRunRequest{
		RunRequest: openai.RunRequest{
			AssistantID: assistantId,
			Model:       openai.GPT4Turbo,
		},
		Thread: openai.ThreadRequest{
			Messages: []openai.ThreadMessage{
				{
					Role: openai.ThreadMessageRoleUser,
					Content: fmt.Sprintf(`
請依以下資訊及需求總結出有用的內容。
當下時間:%s
資訊:
{%s}
輸出規則:台灣各區域天氣總結及當下穿衣建議並考慮時間點，並不要使用任何codeblock，只輸出html。區域後面挑選適合的天氣emoji[ 🌤️ ⛅ 🌥️ 🌦️ ☁️ 🌧️ ⛈️ 🌩️ ☀️]
輸出範例:
<h2>天氣</h2>
<p>更新時間: <time>{當下時間}</time></p>
<img src="https://cwaopendata.s3.ap-northeast-1.amazonaws.com/Observation/O-C0042-002.jpg" />
<h3>北部 {emoji}</h3>
<p>{天氣資訊及穿衣建議}</p>
<h3>中部 {emoji}</h3>
<p>{天氣資訊及穿衣建議}</p>
<h3>南部 {emoji}</h3>
<p>{天氣資訊及穿衣建議}</p>
<h3>東部 {emoji}</h3>
<p>{天氣資訊及穿衣建議}</p>
<h3>外島 {emoji}</h3>
<p>{天氣資訊及穿衣建議}</p>
`, time.Now().In(loc).Format(time.DateTime), weather.String()),
				},
			},
		},
	})
	if err != nil {
		return "", err
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

loop:
	for range ticker.C {
		run, err := client.RetrieveRun(ctx, run.ThreadID, run.ID)
		if err != nil {
			return "", nil
		}

		switch run.Status {
		case openai.RunStatusQueued:
			fallthrough
		case openai.RunStatusInProgress:
			continue
		case openai.RunStatusFailed:
			return "", errors.New(run.LastError.Message)
		case openai.RunStatusCompleted:
			log.Printf("cost: %.2f TWD", getCost(run.Usage))
			break loop
		}
	}

	order := "desc"
	limit := 1
	messages, err := client.ListMessage(ctx, run.ThreadID, &limit, &order, nil, nil)

	if err != nil {
		return "", nil
	}

	if len(messages.Messages) < 1 {
		return "", errors.New("no message found")
	}

	return messages.Messages[0].Content[0].Text.Value, nil
}

func getCost(usage openai.Usage) float32 {
	return (float32(usage.PromptTokens)*0.01 + float32(usage.CompletionTokens)*0.03) * 32 / 1000
}

func createRandomString() string {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		log.Panic(err)
	}

	return base64.URLEncoding.EncodeToString(bytes)
}

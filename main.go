package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sashabaranov/go-openai"

	"53f05cf6/source"
)

var (
	loc, _      = time.LoadLocation("Asia/Taipei")
	port        = "8080"
	openAiToken string
	assistantId string
	cwaToken    string
	logFile     string
	cfg         openai.ClientConfig
)

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

			now := time.Now()
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

			time.Sleep(sleepDuration)
		}
	}()

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("index.html")
		if err != nil {
			log.Fatal(err)
		}

		err = tmpl.Execute(w, template.HTML(dp))
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

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func createDefaultPage() (string, error) {
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
			log.Println(run.Usage)
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

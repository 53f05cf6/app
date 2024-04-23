package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"

	"53f05cf6/weather"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if port == "80" {
		logFile, err := os.OpenFile("./log", os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		if err != nil {
			log.Fatal(err)
		}
		log.SetOutput(logFile)
	}

	openAiToken := os.Getenv("OPENAI_TOKEN")
	if openAiToken == "" {
		log.Fatal("OpenAI token missing")
	}

	if os.Getenv("CWA_TOKEN") == "" {
		log.Fatal("CWA token missing")
	}

	http.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := template.ParseFiles("index.html", "./component/chat-response.html")
		if err != nil {
			log.Fatal(err)
		}

		err = tmpl.Execute(w, nil)
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

	http.HandleFunc("GET /chat/{$}", func(w http.ResponseWriter, r *http.Request) {
		queries := r.URL.Query()

		if queries.Get("test") == "true" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")
			msg := ""
			for i := 0; i < 100; i++ {
				msg += "go "

				time.Sleep(time.Second / 10)
				fmt.Fprintf(w, "data: %s\n\n", msg)
				w.(http.Flusher).Flush()
			}

			fmt.Fprint(w, "event: end\ndata: \n\n")
			w.(http.Flusher).Flush()

			closeNotify := w.(http.CloseNotifier).CloseNotify()
			<-closeNotify
			return
		}

		cwaWeek := weather.CwaWeek{}
		err := cwaWeek.Get()
		if err != nil {
			log.Panic(err)
		}

		loc, _ := time.LoadLocation("Asia/Taipei")
		now := time.Now().In(loc)
		systemPrompt := fmt.Sprintf(`
你是台灣人的助理天氣app助理，請依照"當下時間","一週天氣預報csv"與"用戶的prompt"給出合適的穿衣建議。
當下時間:%s
一週天氣預報csv:
%s
遵守以下規則:
1. 使用台灣正體中文及html: <p>{當前天氣}</p><p>{穿衣建議}</p>
2. 不預設用戶資訊所在地點
3. 如果用戶輸入並非與功能相關則拒絕回答。
4. 盡可能滿足用戶需求。
`, now, cwaWeek.Csv())

		client := openai.NewClient(openAiToken)
		stream, err := client.CreateChatCompletionStream(context.Background(),
			openai.ChatCompletionRequest{
				Model: openai.GPT4Turbo,
				Messages: []openai.ChatCompletionMessage{
					{
						Role:    openai.ChatMessageRoleSystem,
						Content: systemPrompt,
					},
					{
						Role:    openai.ChatMessageRoleUser,
						Content: queries.Get("prompt"),
					},
				},
				Stream: true,
			},
		)
		if err != nil {
			log.Panic(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		defer stream.Close()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		msg := ""
		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				fmt.Fprint(w, "event: end\ndata: end\n\n")
				w.(http.Flusher).Flush()
				break
			}

			if err != nil {
				log.Panic(err)
				break
			}

			msg += response.Choices[0].Delta.Content
			msg = strings.ReplaceAll(msg, "\n", "")

			fmt.Fprintf(w, "data: %s\n\n", msg)
			w.(http.Flusher).Flush()
		}

		<-r.Context().Done()
	})

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}

}

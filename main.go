package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/sashabaranov/go-openai"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	openAiToken := os.Getenv("OPENAI_TOKEN")
	if openAiToken == "" {
		log.Fatal("OpenAI token missing")
	}

	cwaToken := os.Getenv("CWA_TOKEN")
	if cwaToken == "" {
		log.Fatal("OpenAI token missing")
	}

	http.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			tmpl, err := template.ParseFiles("index.html")
			if err != nil {
				log.Fatal(err)
			}
			err = tmpl.Execute(w, nil)
			if err != nil {
				log.Fatal(err)
			}
		}
	})

	http.HandleFunc("/weather/{$}", func(w http.ResponseWriter, r *http.Request) {
		queries := r.URL.Query()
		switch r.Method {
		case http.MethodGet:
			client := openai.NewClient(openAiToken)
			t := time.Now().String()
			weather, err := http.Get("https://opendata.cwa.gov.tw/api/v1/rest/datastore/O-A0003-001?Authorization=" + cwaToken)
			if err != nil {
				log.Fatal("%v", err)
			}
			defer weather.Body.Close()
			body, err := io.ReadAll(weather.Body)
			cwa := Cwa{}
			json.Unmarshal(body, &cwa)

			var station Station

			for _, s := range cwa.Records.Stations {
				if s.GeoInfo.TownName == "中正區" {
					station = s
				}
			}

			bytes, err := json.Marshal(station)
			if err != nil {
				log.Fatal("%v", err)
			}

			msgs := []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "你是一個台灣天氣app助理，你需要依照'當下時間'及'天氣數據'給出合適的穿衣建議;當下時間:" + t + ";天氣數據:" + string(bytes) + ";使用台灣正體中文;第一段為條列天氣數據第二段為穿衣建議及理由: <ul><li>中正區...</li><li>...</li><li>...</li></ul><p>...</p>，但是如果用戶提供偏好的介面需要因此做出用戶指定的回答。如果用戶輸入並非與功能相關則拒絕回答。",
				},
			}

			customMsg := queries.Get("custom")
			if customMsg != "" {
				msgs = append(msgs, openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleUser,
					Content: customMsg,
				})

			}

			resp, err := client.CreateChatCompletion(
				context.Background(),
				openai.ChatCompletionRequest{
					Model:    openai.GPT4Turbo,
					Messages: msgs,
				},
			)

			if err != nil {
				fmt.Printf("ChatCompletion error: %v\n", err)
				return
			}

			snippet := resp.Choices[0].Message.Content

			w.Write([]byte(snippet))
		}

	})

	http.HandleFunc("/news/{$}", func(w http.ResponseWriter, r *http.Request) {
		queries := r.URL.Query()
		switch r.Method {
		case http.MethodGet:
			loc, _ := time.LoadLocation("Asia/Taipei")
			time.Now().In(loc)
			now := time.Now().Format(time.RFC3339)[0:10]
			f, err := os.ReadFile("./news/" + now + ".csv")
			if err != nil {
				log.Fatal("os.ReadFile failed")
			}

			msgs := []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: "你是一個新聞app，你需要依照一份csv檔給出合適的重點新聞;檔案格式為:source,news,link;檔案內容:" + string(f) + ";使用台灣正體中文;預設的輸出為一個html snippet:<p>{回應用戶顯示列表的總結及理由}</p><h2><a href=\"{link}\">{依照內文產生的一句總結}</a></h2><footer>{source}</footer>，但是如果用戶提供偏好的篩選或介面需要因此做出用戶指定的回答。如果用戶輸入並非與功能相關則拒絕回答。",
				},
			}

			customMsg := queries.Get("custom")
			if customMsg != "" {
				msgs = append(msgs, openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleUser,
					Content: customMsg,
				})

			}

			client := openai.NewClient(openAiToken)
			resp, err := client.CreateChatCompletion(
				context.Background(),
				openai.ChatCompletionRequest{
					Model:    openai.GPT4Turbo,
					Messages: msgs,
				},
			)

			if err != nil {
				fmt.Printf("ChatCompletion error: %v\n", err)
				return
			}

			snippet := resp.Choices[0].Message.Content

			w.Write([]byte(snippet))
		}

	})

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}

}

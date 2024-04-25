package main

import (
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

	"github.com/sashabaranov/go-openai"

	"53f05cf6/weather"
)

type Param struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required"`
}

type Prop struct {
	Type        string   `json:"type"`
	Enum        []string `json:"enum"`
	Description string   `json:"description"`
}

type ArrayProp struct {
	Type  string `json:"type"`
	Items struct {
		Type string   `json:"type"`
		Enum []string `json:"enum"`
	} `json:"items"`
	Description string `json:"description"`
}

type Loc struct {
	Locations []string `json:"locations"`
}

type Feature struct {
	Feature string `json:"feature"`
}

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
		ctx := r.Context()

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

			<-r.Context().Done()
			return
		}

		log.Printf("prompt: %s\n", queries.Get("prompt"))

		client := openai.NewClient(openAiToken)
		res, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
			Model: openai.GPT3Dot5Turbo,
			Messages: []openai.ChatCompletionMessage{
				{
					Role: openai.ChatMessageRoleSystem,
					Content: `

你是一個服務台灣人的AI助理的第一個流程:
可以幫忙的的事情有以下:
1.台灣的天氣跟外出穿衣建議
遵守以下規則:
如果同時詢問多筆資訊或不相關的事則拒絕回答
`,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: queries.Get("prompt"),
				},
			},
			Tools: []openai.Tool{
				{
					Type: openai.ToolTypeFunction,
					Function: &openai.FunctionDefinition{
						Name:        "get_request",
						Description: "用戶正在詢問什麼事情",
						Parameters: Param{
							Type: "object",
							Properties: map[string]interface{}{
								"feature": Prop{
									Type:        "string",
									Enum:        []string{"天氣穿衣"},
									Description: "用戶所詢問的事情",
								},
							},
							Required: []string{"feature"},
						},
					},
				},
			},
		})
		if err != nil {
			log.Panic(err)
		}

		if len(res.Choices[0].Message.ToolCalls) == 0 {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(res.Choices[0].Message.Content, "\n", " "))
			log.Printf("reject:1: %s", res.Choices[0].Message.Content)
			w.(http.Flusher).Flush()

			fmt.Fprint(w, "event: end\ndata: \n\n")
			w.(http.Flusher).Flush()

			<-r.Context().Done()
			return
		}

		f := Feature{}
		json.Unmarshal([]byte(res.Choices[0].Message.ToolCalls[0].Function.Arguments), &f)
		log.Println(f.Feature)
		switch f.Feature {

		case "天氣穿衣":
			res, err := client.CreateChatCompletion(ctx,
				openai.ChatCompletionRequest{
					Model: openai.GPT3Dot5Turbo,
					Messages: []openai.ChatCompletionMessage{
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: "根據用戶提供的prompt判斷需要的地點的天氣資訊並呼叫get_weather_info。可能會需要多筆location資訊。'所有'台'都轉換成'臺'。如果有名字但無'縣'或'市'則使用'市'。如果沒有則要求用戶輸入。",
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: queries.Get("prompt"),
						},
					},
					Tools: []openai.Tool{
						{
							Type: openai.ToolTypeFunction,
							Function: &openai.FunctionDefinition{
								Name: "get_locations_weather",
								Parameters: Param{
									Type: "object",
									Properties: map[string]interface{}{
										"locations": ArrayProp{
											Type: "array",
											Items: struct {
												Type string   `json:"type"`
												Enum []string `json:"enum"`
											}{
												Type: "string",
												Enum: []string{"宜蘭縣", "花蓮縣", "臺東縣", "澎湖縣", "金門縣", "連江縣", "臺北市", "新北市", "桃園市", "臺中市", "臺南市", "高雄市", "基隆市", "新竹縣", "新竹市", "苗栗縣", "彰化縣", "南投縣", "雲林縣", "嘉義縣", "嘉義市", "屏東縣"},
											},
											Description: "多個地點",
										},
									},
									Required: []string{"locations"},
								},
							},
						},
					},
				},
			)
			if err != nil {
				log.Panic(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			if len(res.Choices[0].Message.ToolCalls) == 0 {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Connection", "keep-alive")

				fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(res.Choices[0].Message.Content, "\n", ""))
				log.Printf("reject:2: %s", res.Choices[0].Message.Content)
				w.(http.Flusher).Flush()

				fmt.Fprint(w, "event: end\ndata: \n\n")
				w.(http.Flusher).Flush()

				<-r.Context().Done()
				return
			}

			cwaWeek := weather.CwaWeek{}
			funcParam := Loc{}
			json.Unmarshal([]byte(res.Choices[0].Message.ToolCalls[0].Function.Arguments), &funcParam)
			log.Println(funcParam.Locations)
			err = cwaWeek.Get(funcParam.Locations)
			if err != nil {
				log.Panic(err)
			}

			loc, _ := time.LoadLocation("Asia/Taipei")
			now := time.Now().In(loc)
			stream, err := client.CreateChatCompletionStream(ctx,
				openai.ChatCompletionRequest{
					Model: openai.GPT4Turbo,
					Messages: []openai.ChatCompletionMessage{
						{
							Role: openai.ChatMessageRoleSystem,
							Content: fmt.Sprintf(`
你是台灣人的助理天氣app助理，請依照"當下時間","一週天氣預報csv"與"用戶的prompt"給出合適的穿衣建議。
當下時間:%s
一週天氣預報csv: %s 
遵守以下規則: 1. 使用台灣正體中文及html: <p>{當前天氣}</p><p>{穿衣建議}</p>
`, now, cwaWeek.Csv()),
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
					fmt.Fprint(w, "event: end\ndata: \n\n")
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

		// case "新聞":
		// 	<-r.Context().Done()
		default:
			<-r.Context().Done()
		}

	})

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}

}

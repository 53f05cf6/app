package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

func main() {
	now := time.Now()
	todayStr := now.Format(time.RFC3339)[0:10]
	_, _, nowDay := time.Now().Date()
	tail := ""
	fp := gofeed.NewParser()
	feed, _ := fp.ParseURL("https://www.mohw.gov.tw/rss-16-1.html")
	for _, item := range feed.Items {
		_, _, newsDay := item.PublishedParsed.Date()
		if nowDay-1 != newsDay {
			continue
		}

		desc := strings.ReplaceAll(item.Description, "\n", "")
		tail += fmt.Sprintf("\"衛生福利部-%s\",\"%s:%s\",\"%s\"\n", item.Custom["DeptName"], item.Title, desc, item.Link)
	}

	f, err := os.OpenFile(fmt.Sprintf("news/%s.csv", todayStr), os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		log.Fatal("os.Open failed")
	}

	if _, err := f.WriteString(tail); err != nil {
		log.Fatal("f.WriteString failed: ", err)
	}
}

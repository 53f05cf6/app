package main

import "time"

type News struct {
	ID        int
	Title     string
	Content   string
	CreatedAt time.Time
}

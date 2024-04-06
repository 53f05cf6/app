package main

import "time"

type News struct {
	ID        int
	Title     string
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (n News) Format() string {
	return n.CreatedAt.Format("2006-01-02")
}

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

type Cwa struct {
	Records struct {
		Stations []Station `json:"Station"`
	}
}

type Station struct {
	GeoInfo struct {
		CountyName string
		TownName   string
	}
	WeatherElement struct {
		Weather               string
		VisibilityDescription string
		SunshineDuration      float32
		WindDirection         float32
		WindSpeed             float32
		AirTemperature        float32
		RelativeHumidity      float32
	}
}

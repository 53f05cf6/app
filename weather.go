package main

import "fmt"

type CwaWeek struct {
	Records struct {
		Locations []struct {
			Location []Location `json:"location"`
		} `json:"locations"`
	} `json:"records"`
}

type Location struct {
	LocationName   string `json:"locationName"`
	WeatherElement []struct {
		ElementName string `json:"elementName"`
		Description string `json:"description"`
		Time        []struct {
			StartTime    string `json:"startTime"`
			ElementValue []struct {
				Value    string `json:"value"`
				Measures string `json:"measures"`
			} `json:"elementValue"`
		} `json:"time"`
	}
}

type Row struct {
	PoP12h string
	T      string
	RH     string
	MinCI  string
	WS     string
	MaxAT  string
	Wx     string
	MaxCI  string
	MinT   string
	UVI    string
	MinAT  string
	MaxT   string
}

func (r *Row) String() string {
	uvi := "n/a"
	if r.UVI != "" {
		uvi = r.UVI
	}
	return fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s", r.PoP12h, r.T, r.RH, r.MinCI, r.WS, r.MaxAT, r.Wx, r.MaxCI, r.MinT, uvi, r.MinAT, r.MaxT)
}

func (cwaWeek CwaWeek) Csv() string {
	csv := "地點,時間,12小時降雨機率,平均溫度,平均相對濕度,最小舒適度指數,最大風速,最高體感溫度,天氣現象,最大舒適度指數,最低溫度,紫外線指數,最低體感溫度,最高溫度\n"
	locations := cwaWeek.Records.Locations[0].Location

	times := []string{}
	for _, t := range locations[0].WeatherElement[0].Time {
		times = append(times, t.StartTime)
	}

	for _, l := range locations {
		m := map[string]*Row{}
		for _, we := range l.WeatherElement {
			for _, t := range we.Time {
				if _, ok := m[t.StartTime]; !ok {
					m[t.StartTime] = &Row{}
				}

				switch we.ElementName {
				case "PoP12h":
					if t.ElementValue[0].Value == " " {
						m[t.StartTime].PoP12h = "n/a"
						continue
					}

					m[t.StartTime].PoP12h = fmt.Sprintf("%s%%", t.ElementValue[0].Value)
				case "T":
					m[t.StartTime].T = fmt.Sprintf("%sC", t.ElementValue[0].Value)
				case "RH":
					m[t.StartTime].RH = fmt.Sprintf("%s%%", t.ElementValue[0].Value)
				case "MinCI":
					// AB test
					m[t.StartTime].MinCI = fmt.Sprintf("%s", t.ElementValue[1].Value)
				case "WS":
					m[t.StartTime].WS = fmt.Sprintf("%s %s", t.ElementValue[1].Value, t.ElementValue[1].Measures)
				case "MaxAT":
					m[t.StartTime].MaxAT = fmt.Sprintf("%sC", t.ElementValue[0].Value)
				case "Wx":
					m[t.StartTime].Wx = fmt.Sprintf("%s", t.ElementValue[0].Value)
				case "MaxCI":
					// AB test
					m[t.StartTime].MaxCI = fmt.Sprintf("%s", t.ElementValue[1].Value)
				case "MinT":
					m[t.StartTime].MinT = fmt.Sprintf("%sC", t.ElementValue[0].Value)
				case "UVI":
					m[t.StartTime].UVI = fmt.Sprintf("%s %s", t.ElementValue[0].Value, t.ElementValue[1].Value)
				case "MinAT":
					m[t.StartTime].MinAT = fmt.Sprintf("%sC", t.ElementValue[0].Value)
				case "MaxT":
					m[t.StartTime].MaxT = fmt.Sprintf("%sC", t.ElementValue[0].Value)
				}

			}
		}

		for _, t := range times {
			csv += fmt.Sprintf("%s,%s,%s\n", l.LocationName, t, m[t].String())
		}
	}

	return csv
}

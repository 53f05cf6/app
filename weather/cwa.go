package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type CwaWeek struct {
	Records struct {
		Locations []struct {
			Location []location `json:"location"`
		} `json:"locations"`
	} `json:"records"`
}

func (cwaWeek *CwaWeek) Get() error {
	cwaToken := os.Getenv("CWA_TOKEN")
	res, err := http.Get("https://opendata.cwa.gov.tw/api/v1/rest/datastore/F-D0047-091?Authorization=" + cwaToken)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	json.Unmarshal(body, cwaWeek)

	return nil
}

func (cwaWeek *CwaWeek) Txt() string {
	txt := ""
	locations := cwaWeek.Records.Locations[0].Location

	times := []string{}
	for _, t := range locations[0].WeatherElement[0].Time {
		times = append(times, t.StartTime)
	}

	for _, l := range locations {
		txt += l.LocationName + "\n"
		m := map[string]*row{}
		for _, we := range l.WeatherElement {
			for _, t := range we.Time {
				if _, ok := m[t.StartTime]; !ok {
					m[t.StartTime] = &row{}
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
			r := m[t]
			uvi := "n/a"
			if r.UVI != "" {
				uvi = r.UVI
			}
			txt += fmt.Sprintf(`時間:%s
12小時降雨機率: %s
平均溫度: %s
平均相對濕度: %s
最小舒適度指數: %s
最大風速: %s
最高體感溫度: %s
天氣現象: %s
最大舒適度指數: %s
最低溫度: %s
紫外線指數: %s
最低體感溫度: %s
最高溫度: %s

`, t, r.PoP12h, r.T, r.RH, r.MinCI, r.WS, r.MaxAT, r.Wx, r.MaxCI, r.MinT, uvi, r.MinAT, r.MaxT)
		}
	}

	return txt
}

func (cwaWeek *CwaWeek) Csv() string {
	csv := "地點,時間,12小時降雨機率,平均溫度,平均相對濕度,最小舒適度指數,最大風速,最高體感溫度,天氣現象,最大舒適度指數,最低溫度,紫外線指數,最低體感溫度,最高溫度\n"
	locations := cwaWeek.Records.Locations[0].Location

	times := []string{}
	for _, t := range locations[0].WeatherElement[0].Time {
		times = append(times, t.StartTime)
	}

	for _, l := range locations {
		m := map[string]*row{}
		for _, we := range l.WeatherElement {
			for _, t := range we.Time {
				if _, ok := m[t.StartTime]; !ok {
					m[t.StartTime] = &row{}
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

type location struct {
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

type row struct {
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

func (r *row) String() string {
	uvi := "n/a"
	if r.UVI != "" {
		uvi = r.UVI
	}
	return fmt.Sprintf("%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s", r.PoP12h, r.T, r.RH, r.MinCI, r.WS, r.MaxAT, r.Wx, r.MaxCI, r.MinT, uvi, r.MinAT, r.MaxT)
}

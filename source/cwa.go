package source

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Forecast36Hours struct {
	Token string
	Raw   Raw
}

type Raw struct {
	Records struct {
		Locations []struct {
			LocationName    string `json:"locationName"`
			WeatherElements []struct {
				ElementName string `json:"elementName"`
				Time        []struct {
					StartTime string `json:"startTime"`
					EndTime   string `json:"endTime"`
					Parameter struct {
						ParameterName string `json:"parameterName"`
						ParameterUnit string `json:"parameterUnit"`
					} `json:"parameter"`
				} `json:"time"`
			} `json:"weatherElement"`
		} `json:"location"`
	} `json:"records"`
}

func (f36h *Forecast36Hours) Get() error {
	url := fmt.Sprintf("https://opendata.cwa.gov.tw/api/v1/rest/datastore/F-C0032-001?Authorization=%s", f36h.Token)
	res, err := http.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	json.Unmarshal(body, &f36h.Raw)

	return nil
}

func (f36h Forecast36Hours) String() string {
	str := ""
	locations := f36h.Raw.Records.Locations
	startTime := locations[0].WeatherElements[0].Time[0].StartTime
	endTime := locations[0].WeatherElements[0].Time[0].EndTime
	str += "台灣分區\n北部:[臺北市,新北市,基隆市,桃園市,新竹市,新竹縣,宜蘭縣]\n"
	str += "中部:[苗栗縣,臺中市,彰化縣,南投縣,雲林縣]=\n"
	str += "南部:[嘉義市,嘉義縣,臺南市,高雄市,屏東縣,澎湖縣]\n"
	str += "東部:[花蓮縣,臺東縣]\n"
	str += "外島:[金門縣,連江縣]\n"
	str += fmt.Sprintf("%s至%s天氣預報\n", startTime, endTime)

	for _, l := range locations {
		str += l.LocationName + ": "

		for _, we := range l.WeatherElements {
			switch we.ElementName {
			case "Wx":
				str += fmt.Sprintf("%s, ", we.Time[0].Parameter.ParameterName)
			case "PoP":
				str += fmt.Sprintf("降雨機率%s%%, ", we.Time[0].Parameter.ParameterName)
			case "MinT":
				str += fmt.Sprintf("最低溫%s°C, ", we.Time[0].Parameter.ParameterName)
			case "MaxT":
				str += fmt.Sprintf("最高溫%s°C, ", we.Time[0].Parameter.ParameterName)
			case "CI":
				str += fmt.Sprintf("%s, ", we.Time[0].Parameter.ParameterName)
			}
		}

		str += "\n"
	}

	return str
}

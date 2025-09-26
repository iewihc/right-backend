package google_map

type LatLngToAddressInput struct {
	Body struct {
		Lat string `json:"lat" example:"25.0330" doc:"緯度"`
		Lng string `json:"lng" example:"121.5654" doc:"經度"`
	} `json:"body" example:"{\"lat\":\"25.0330\",\"lng\":\"121.5654\"}"`
}
type LatLngToAddressOutput struct {
	Address string `json:"address" example:"台北市信義區信義路五段7號"`
}
type LatLngToAddressResponse struct {
	Body LatLngToAddressOutput `json:"body"`
}

type AddressToLatLngInput struct {
	Body struct {
		Address string `json:"address" example:"台北101" doc:"地址"`
	} `json:"body" example:"{\"address\":\"台北101\"}"`
}
type AddressToLatLngOutput struct {
	Lat string `json:"lat" example:"25.0330"`
	Lng string `json:"lng" example:"121.5654"`
}
type AddressToLatLngResponse struct {
	Body AddressToLatLngOutput `json:"body"`
}

type DistanceMatrixInput struct {
	Body struct {
		Origin      string `json:"origin" example:"25.0330,121.5654" doc:"起點經緯度"`
		Destination string `json:"destination" example:"25.0478,121.5170" doc:"終點經緯度"`
	} `json:"body" example:"{\"origin\":\"25.0330,121.5654\",\"destination\":\"25.0478,121.5170\"}"`
}
type DistanceMatrixOutput struct {
	Distance     string  `json:"distance" example:"5.2 公里"`
	Duration     string  `json:"duration" example:"15 分鐘"`
	DistanceKm   float64 `json:"distance_km" example:"5.2"`
	DurationMins int     `json:"duration_mins" example:"15"`
}
type DistanceMatrixResponse struct {
	Body DistanceMatrixOutput `json:"body"`
}

type PlacesAutocompleteInput struct {
	Body struct {
		Input string `json:"input" example:"台北" doc:"搜尋關鍵字"`
	} `json:"body" example:"{\"input\":\"台北\"}"`
}
type PlacesAutocompleteOutput struct {
	Predictions []string `json:"predictions" example:"[\"台北101\",\"台北車站\"]"`
}
type PlacesAutocompleteResponse struct {
	Body PlacesAutocompleteOutput `json:"body"`
}

type NearestDriverInput struct {
	Body struct {
		DriverCoords   []string `json:"driver_coords" example:"[\"25.0330,121.5654\",\"25.0478,121.5170\"]" doc:"司機座標陣列(最多25個)"`
		PassengerCoord string   `json:"passenger_coord" example:"25.0418,121.5500" doc:"乘客上車點座標"`
	} `json:"body"`
}
type NearestDriverOutput struct {
	NearestIndex int    `json:"nearest_index" example:"0" doc:"最近司機在陣列中的索引(0起)"`
	Distance     string `json:"distance" example:"1.2 公里"`
	Duration     string `json:"duration" example:"5 分鐘"`
}

type NearestDriverResponse struct {
	Body NearestDriverOutput `json:"body"`
}

// Find Place from Text API
type FindPlaceInput struct {
	Body struct {
		Input string `json:"input" example:"光田綜合醫院 向上院區 台中" doc:"搜尋關鍵字"`
	} `json:"body"`
}

type FindPlaceCandidate struct {
	PlaceID    string   `json:"place_id" example:"ChIJXXXXXXXXXXXXXXXX"`
	Name       string   `json:"name" example:"光田綜合醫院向上院區"`
	Address    string   `json:"formatted_address" example:"台中市沙鹿區向上路七段125號"`
	Lat        float64  `json:"lat" example:"24.2266"`
	Lng        float64  `json:"lng" example:"120.5689"`
	Rating     float64  `json:"rating,omitempty" example:"4.2"`
	PriceLevel int      `json:"price_level,omitempty" example:"2"`
	Tags       []string `json:"tags,omitempty" example:"[\"XC\",\"X Cube\",\"Cube\",\"XCube\"]"`
	FromCache  bool     `json:"fromCache" example:"true"`
}

type FindPlaceOutput struct {
	Candidates []FindPlaceCandidate `json:"candidates"`
}

type FindPlaceResponse struct {
	Body FindPlaceOutput `json:"body"`
}

// Place Details API
type PlaceDetailsInput struct {
	Body struct {
		PlaceID string `json:"place_id" example:"ChIJXXXXXXXXXXXXXXXX" doc:"Google Place ID"`
	} `json:"body"`
}

type PlaceDetailsOutput struct {
	PlaceID      string   `json:"place_id" example:"ChIJXXXXXXXXXXXXXXXX"`
	Name         string   `json:"name" example:"光田綜合醫院向上院區"`
	Address      string   `json:"formatted_address" example:"台中市沙鹿區向上路七段125號"`
	Lat          float64  `json:"lat" example:"24.2266"`
	Lng          float64  `json:"lng" example:"120.5689"`
	Phone        string   `json:"formatted_phone_number,omitempty" example:"04-2662-5111"`
	Website      string   `json:"website,omitempty" example:"https://www.ktgh.com.tw/"`
	Types        []string `json:"types,omitempty" example:"[\"hospital\",\"health\",\"establishment\"]"`
	Rating       float64  `json:"rating,omitempty" example:"4.2"`
	PriceLevel   int      `json:"price_level,omitempty" example:"2"`
	OpeningHours []string `json:"opening_hours,omitempty" example:"[\"星期一: 08:00–17:30\",\"星期二: 08:00–17:30\"]"`
	OpenNow      bool     `json:"open_now,omitempty" example:"true"`
	Tags         []string `json:"tags,omitempty" example:"[\"XC\",\"X Cube\",\"Cube\",\"XCube\"]"`
	FromCache    bool     `json:"fromCache" example:"true"`
}

type PlaceDetailsResponse struct {
	Body PlaceDetailsOutput `json:"body"`
}

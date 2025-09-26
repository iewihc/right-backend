package crawler

import "right-backend/service"

// GmapsAutocompleteInput is the input for the gmaps autocomplete endpoint.
type GmapsAutocompleteInput struct {
	Body struct {
		Keyword string `json:"keyword" doc:"要搜尋的關鍵字"`
	}
}

// GmapsAutocompleteOutput is the output for the gmaps autocomplete endpoint.
type GmapsAutocompleteOutput struct {
	Body struct {
		Suggestions []service.Suggestion `json:"suggestions" doc:"搜尋建議結果列表"`
	}
}

// GmatrixInverseInput is the input for the gmaps directions matrix inverse endpoint.
type GmatrixInverseInput struct {
	Body struct {
		Origins     []string `json:"origins" doc:"起點地址列表"`
		Destination string   `json:"destination" doc:"目的地地址"`
	}
}

// GmatrixInverseOutput is the output for the gmaps directions matrix inverse endpoint.
type GmatrixInverseOutput struct {
	Body struct {
		Routes []service.RouteInfo `json:"routes" doc:"路線資訊列表"`
	}
}

// GmapsGeoInput is the input for the gmaps address to geo endpoint.
type GmapsGeoInput struct {
	Body struct {
		Keyword string `json:"keyword" doc:"要轉換為經緯度的地址或關鍵字"`
	}
}

// GmapsGeoOutput is the output for the gmaps address to geo endpoint.
type GmapsGeoOutput struct {
	Body service.GeoPoint `json:"geo"`
}

// GmapsAllDirectionsInput is the input for the gmaps all directions endpoint.
type GmapsAllDirectionsInput struct {
	Body struct {
		Origin      string `json:"origin" doc:"起點地址"`
		Destination string `json:"destination" doc:"目的地地址"`
	}
}

// GmapsAllDirectionsOutput is the output for the gmaps all directions endpoint.
type GmapsAllDirectionsOutput struct {
	Body struct {
		Routes []service.RouteInfo `json:"routes" doc:"所有路線資訊列表"`
	}
}

// GmapsDetailsInput is the input for the gmaps details endpoint.
type GmapsDetailsInput struct {
	Body struct {
		Keyword string `json:"keyword" doc:"要搜尋並抓取詳細資料的關鍵字"`
	}
}

// GmapsDetailsOutput is the output for the gmaps details endpoint.
type GmapsDetailsOutput struct {
	Body service.LocationDetails `json:"details"`
}

// GmapsDirectionsInput is the input for the gmaps directions endpoint.
type GmapsDirectionsInput struct {
	Body struct {
		Start string `json:"start" doc:"起點地址"`
		End   string `json:"end" doc:"終點地址"`
	}
}

// GmapsDirectionsOutput is the output for the gmaps directions endpoint.
type GmapsDirectionsOutput struct {
	Body []service.RouteInfo `json:"routes"`
}

// GmapsAutoSuggestV2Input is the input for the gmaps auto suggest v2 endpoint.
type GmapsAutoSuggestV2Input struct {
	Body struct {
		Keyword string `json:"keyword" doc:"要搜尋的關鍵字"`
	}
}

// GmapsAutoSuggestV2Output is the output for the gmaps auto suggest v2 endpoint.
type GmapsAutoSuggestV2Output struct {
	Body struct {
		Suggestions []service.SuggestionV2 `json:"suggestions" doc:"搜尋結果列表，包含名稱和地址"`
	}
}

// GmapsSearchDetailV2Input is the input for the gmaps search detail v2 endpoint.
type GmapsSearchDetailV2Input struct {
	Body struct {
		Address string `json:"address" doc:"要查找完整資訊的地址"`
	}
}

// GmapsSearchDetailV2Output is the output for the gmaps search detail v2 endpoint.
type GmapsSearchDetailV2Output struct {
	Body struct {
		FullAddress string `json:"full_address" doc:"完整地址資訊"`
	}
}

// GmapsDetailsV3Input is the input for the gmaps details v3 endpoint.
type GmapsDetailsV3Input struct {
	Body struct {
		Keyword string `json:"keyword" doc:"要搜尋的地址關鍵字"`
	}
}

// GmapsDetailsV3Output is the output for the gmaps details v3 endpoint.
type GmapsDetailsV3Output struct {
	Body struct {
		Address string `json:"address" doc:"抓取到的最佳地址（優先前面帶數字的地址）"`
		Lat     string `json:"lat" doc:"緯度"`
		Lng     string `json:"lng" doc:"經度"`
	}
}

package controller

import (
	"context"
	"right-backend/data-models/google_map"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

type GoogleMapController struct {
	logger        zerolog.Logger
	GoogleService *service.GoogleMapService
}

func NewGoogleMapController(logger zerolog.Logger, svc *service.GoogleMapService) *GoogleMapController {
	return &GoogleMapController{
		logger:        logger.With().Str("module", "google_map_controller").Logger(),
		GoogleService: svc,
	}
}

func (c *GoogleMapController) RegisterRoutes(api huma.API) {
	// 1. 經緯度轉地址
	//huma.Register(api, huma.Operation{
	//	OperationID: "latlng-to-address",
	//	Tags:        []string{"google"},
	//	Summary:     "經緯度轉地址",
	//	Method:      "POST",
	//	Path:        "/google/latlng-to-address",
	//}, func(ctx context.Context, input *google_map.LatLngToAddressInput) (*google_map.LatLngToAddressResponse, error) {
	//	addr, err := c.GoogleService.GeocodeLatLng(ctx, input.Body.Lat, input.Body.Lng)
	//	if err != nil {
	//		c.logger.Error().Err(err).Str("lat", input.Body.Lat).Str("lng", input.Body.Lng).Msg("查詢失敗")
	//		return nil, huma.Error400BadRequest("查詢失敗", err)
	//	}
	//	return &google_map.LatLngToAddressResponse{Body: google_map.LatLngToAddressOutput{Address: addr}}, nil
	//})

	// 2. 地址轉經緯度
	//huma.Register(api, huma.Operation{
	//	OperationID: "address-to-latlng",
	//	Tags:        []string{"google"},
	//	Summary:     "地址轉經緯度",
	//	Method:      "POST",
	//	Path:        "/google/address-to-latlng",
	//}, func(ctx context.Context, input *google_map.AddressToLatLngInput) (*google_map.AddressToLatLngResponse, error) {
	//	lat, lng, err := c.GoogleService.GeocodeAddress(ctx, input.Body.Address)
	//	if err != nil {
	//		c.logger.Error().Err(err).Str("address", input.Body.Address).Msg("查詢失敗")
	//		return nil, huma.Error400BadRequest("查詢失敗", err)
	//	}
	//	return &google_map.AddressToLatLngResponse{Body: google_map.AddressToLatLngOutput{Lat: lat, Lng: lng}}, nil
	//})

	// 3. 計算兩點間道路距離與預估時間
	//huma.Register(api, huma.Operation{
	//	OperationID: "distance-matrix",
	//	Tags:        []string{"google"},
	//	Summary:     "計算兩點間道路距離與預估時間",
	//	Method:      "POST",
	//	Path:        "/google/distance-matrix",
	//}, func(ctx context.Context, input *google_map.DistanceMatrixInput) (*google_map.DistanceMatrixResponse, error) {
	//	distKm, durMins, err := c.GoogleService.DistanceMatrix(ctx, input.Body.Origin, input.Body.Destination)
	//	if err != nil {
	//		c.logger.Error().Err(err).Str("origin", input.Body.Origin).Str("destination", input.Body.Destination).Msg("查詢失敗")
	//		return nil, huma.Error400BadRequest("查詢失敗", err)
	//	}
	//	// 為了向後相容，同時提供格式化字串和純數字
	//	distStr := fmt.Sprintf("%.1f 公里", distKm)
	//	durStr := fmt.Sprintf("%d 分鐘", durMins)
	//	return &google_map.DistanceMatrixResponse{Body: google_map.DistanceMatrixOutput{
	//		Distance:     distStr,
	//		Duration:     durStr,
	//		DistanceKm:   distKm,
	//		DurationMins: durMins,
	//	}}, nil
	//})

	// 4. Google Places 自動完成搜尋
	huma.Register(api, huma.Operation{
		OperationID: "places-autocomplete",
		Tags:        []string{"google"},
		Summary:     "Google Places 自動完成搜尋",
		Method:      "POST",
		Path:        "/google/places-autocomplete",
	}, func(ctx context.Context, input *google_map.PlacesAutocompleteInput) (*google_map.PlacesAutocompleteResponse, error) {
		preds, err := c.GoogleService.PlacesAutocomplete(ctx, input.Body.Input)
		if err != nil {
			c.logger.Error().Err(err).Str("input", input.Body.Input).Msg("查詢失敗")
			return nil, huma.Error400BadRequest("查詢失敗", err)
		}
		return &google_map.PlacesAutocompleteResponse{Body: google_map.PlacesAutocompleteOutput{Predictions: preds}}, nil
	})

	// 5. 找出距離乘客最近的司機 (最多25個司機)
	//huma.Register(api, huma.Operation{
	//	OperationID: "find-nearest-driver",
	//	Tags:        []string{"google"},
	//	Summary:     "找出距離乘客最近的司機 (最多25個司機)",
	//	Method:      "POST",
	//	Path:        "/google/nearest-driver",
	//}, func(ctx context.Context, input *google_map.NearestDriverInput) (*google_map.NearestDriverResponse, error) {
	//	idx, dist, dur, err := c.GoogleService.FindNearestDriver(ctx, input.Body.DriverCoords, input.Body.PassengerCoord)
	//	if err != nil {
	//		c.logger.Error().Err(err).Msg("查詢失敗")
	//		return nil, huma.Error400BadRequest("查詢失敗", err)
	//	}
	//	return &google_map.NearestDriverResponse{Body: google_map.NearestDriverOutput{NearestIndex: idx, Distance: dist, Duration: dur}}, nil
	//})

	// 6. Google Places Find Place from Text API
	huma.Register(api, huma.Operation{
		OperationID: "find-place-from-text",
		Tags:        []string{"google"},
		Summary:     "根據文字搜尋地點（Find Place from Text）",
		Method:      "POST",
		Path:        "/google/find-place",
	}, func(ctx context.Context, input *google_map.FindPlaceInput) (*google_map.FindPlaceResponse, error) {
		candidates, err := c.GoogleService.FindPlaceFromText(ctx, input.Body.Input)
		if err != nil {
			c.logger.Error().Err(err).Str("input", input.Body.Input).Msg("查詢失敗")
			return nil, huma.Error400BadRequest("查詢失敗", err)
		}

		var result []google_map.FindPlaceCandidate
		for _, candidate := range candidates {
			if candidateMap, ok := candidate.(map[string]interface{}); ok {
				place := google_map.FindPlaceCandidate{}

				if placeID, ok := candidateMap["place_id"].(string); ok {
					place.PlaceID = placeID
				}
				if name, ok := candidateMap["name"].(string); ok {
					place.Name = name
				}
				if address, ok := candidateMap["formatted_address"].(string); ok {
					place.Address = address
				}
				if geometry, ok := candidateMap["geometry"].(map[string]interface{}); ok {
					if location, ok := geometry["location"].(map[string]interface{}); ok {
						if lat, ok := location["lat"].(float64); ok {
							place.Lat = lat
						}
						if lng, ok := location["lng"].(float64); ok {
							place.Lng = lng
						}
					}
				}
				if rating, ok := candidateMap["rating"].(float64); ok {
					place.Rating = rating
				}
				if priceLevel, ok := candidateMap["price_level"].(float64); ok {
					place.PriceLevel = int(priceLevel)
				}
				if tags, ok := candidateMap["tags"].([]interface{}); ok {
					place.Tags = make([]string, len(tags))
					for i, tag := range tags {
						if tagStr, ok := tag.(string); ok {
							place.Tags[i] = tagStr
						}
					}
				}
				if fromCache, ok := candidateMap["fromCache"].(bool); ok {
					place.FromCache = fromCache
				}

				result = append(result, place)
			}
		}

		return &google_map.FindPlaceResponse{Body: google_map.FindPlaceOutput{Candidates: result}}, nil
	})

	// 7. Google Places Place Details API
	huma.Register(api, huma.Operation{
		OperationID: "place-details",
		Tags:        []string{"google"},
		Summary:     "根據 Place ID 獲取地點詳細資訊",
		Method:      "POST",
		Path:        "/google/place-details",
	}, func(ctx context.Context, input *google_map.PlaceDetailsInput) (*google_map.PlaceDetailsResponse, error) {
		placeResult, err := c.GoogleService.GetPlaceDetails(ctx, input.Body.PlaceID)
		if err != nil {
			c.logger.Error().Err(err).Str("place_id", input.Body.PlaceID).Msg("查詢失敗")
			return nil, huma.Error400BadRequest("查詢失敗", err)
		}

		result := google_map.PlaceDetailsOutput{}

		if placeID, ok := placeResult["place_id"].(string); ok {
			result.PlaceID = placeID
		}
		if name, ok := placeResult["name"].(string); ok {
			result.Name = name
		}
		if address, ok := placeResult["formatted_address"].(string); ok {
			result.Address = address
		}
		if geometry, ok := placeResult["geometry"].(map[string]interface{}); ok {
			if location, ok := geometry["location"].(map[string]interface{}); ok {
				if lat, ok := location["lat"].(float64); ok {
					result.Lat = lat
				}
				if lng, ok := location["lng"].(float64); ok {
					result.Lng = lng
				}
			}
		}
		if phone, ok := placeResult["formatted_phone_number"].(string); ok {
			result.Phone = phone
		}
		if website, ok := placeResult["website"].(string); ok {
			result.Website = website
		}
		if types, ok := placeResult["types"].([]interface{}); ok {
			result.Types = make([]string, len(types))
			for i, t := range types {
				if typeStr, ok := t.(string); ok {
					result.Types[i] = typeStr
				}
			}
		}
		if rating, ok := placeResult["rating"].(float64); ok {
			result.Rating = rating
		}
		if priceLevel, ok := placeResult["price_level"].(float64); ok {
			result.PriceLevel = int(priceLevel)
		}
		if openingHours, ok := placeResult["opening_hours"].(map[string]interface{}); ok {
			if weekdayText, ok := openingHours["weekday_text"].([]interface{}); ok {
				result.OpeningHours = make([]string, len(weekdayText))
				for i, day := range weekdayText {
					if dayStr, ok := day.(string); ok {
						result.OpeningHours[i] = dayStr
					}
				}
			}
			if openNow, ok := openingHours["open_now"].(bool); ok {
				result.OpenNow = openNow
			}
		}
		if tags, ok := placeResult["tags"].([]interface{}); ok {
			result.Tags = make([]string, len(tags))
			for i, tag := range tags {
				if tagStr, ok := tag.(string); ok {
					result.Tags[i] = tagStr
				}
			}
		}
		if fromCache, ok := placeResult["fromCache"].(bool); ok {
			result.FromCache = fromCache
		}

		return &google_map.PlaceDetailsResponse{Body: result}, nil
	})
}

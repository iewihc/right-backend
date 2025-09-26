package controller

import (
	"context"
	"net/http"
	"right-backend/data-models/crawler"
	"right-backend/infra"
	"right-backend/service"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog"
)

// CrawlerController is the controller for crawler related operations.
type CrawlerController struct {
	logger         zerolog.Logger
	crawlerService *service.CrawlerService
}

// NewCrawlerController creates a new CrawlerController.
func NewCrawlerController(logger zerolog.Logger, crawlerService *service.CrawlerService) *CrawlerController {
	return &CrawlerController{
		logger:         logger.With().Str("module", "crawler_controller").Logger(),
		crawlerService: crawlerService,
	}
}

// RegisterRoutes registers the routes for the CrawlerController.
func (c *CrawlerController) RegisterRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "gmaps-directions-matrix-inverse",
		Method:      http.MethodPost,
		Path:        "/crawler/gmaps-directions-matrix-inverse",
		Summary:     "Google Maps 多點到單點路線規劃",
		Tags:        []string{"crawler"},
	}, func(ctx context.Context, input *crawler.GmatrixInverseInput) (*crawler.GmatrixInverseOutput, error) {
		ctx, span := infra.StartSpan(ctx, "crawler_directions_matrix_inverse",
			infra.AttrOperation("directions_matrix_inverse"),
			infra.AttrInt("origins_count", len(input.Body.Origins)),
			infra.AttrString("destination", input.Body.Destination),
		)
		defer span.End()

		infra.AddEvent(span, "directions_matrix_inverse_started",
			infra.AttrInt("origins_count", len(input.Body.Origins)),
			infra.AttrString("destination", input.Body.Destination),
		)

		if len(input.Body.Origins) == 0 {
			infra.RecordError(span, huma.Error400BadRequest("origins cannot be empty"), "缺少必要的 origins 參數",
				infra.AttrString("error", "origins cannot be empty"),
			)
			c.logger.Warn().Msg("缺少必要的 origins 參數")
			return nil, huma.Error400BadRequest("origins cannot be empty")
		}
		if input.Body.Destination == "" {
			infra.RecordError(span, huma.Error400BadRequest("destination cannot be empty"), "缺少必要的 destination 參數",
				infra.AttrString("error", "destination cannot be empty"),
			)
			c.logger.Warn().Msg("缺少必要的 destination 參數")
			return nil, huma.Error400BadRequest("destination cannot be empty")
		}

		routes, err := c.crawlerService.DirectionsMatrixInverse(ctx, input.Body.Origins, input.Body.Destination)
		if err != nil {
			infra.RecordError(span, err, "多點到單點路線規劃執行失敗",
				infra.AttrInt("origins_count", len(input.Body.Origins)),
				infra.AttrString("destination", input.Body.Destination),
				infra.AttrString("error", err.Error()),
			)
			c.logger.Error().Err(err).Int("origins_count", len(input.Body.Origins)).Str("destination", input.Body.Destination).Msg("多點到單點路線規劃執行失敗")
			return nil, huma.Error500InternalServerError("Failed to execute directions matrix inverse search")
		}

		infra.AddEvent(span, "directions_matrix_inverse_completed",
			infra.AttrInt("routes_count", len(routes)),
		)
		infra.MarkSuccess(span,
			infra.AttrInt("origins_count", len(input.Body.Origins)),
			infra.AttrString("destination", input.Body.Destination),
			infra.AttrInt("routes_count", len(routes)),
		)

		resp := &crawler.GmatrixInverseOutput{}
		resp.Body.Routes = routes
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "gmaps-all-directions",
		Method:      http.MethodPost,
		Path:        "/crawler/gmaps-all-directions",
		Summary:     "Google Maps 兩點間所有路線規劃",
		Tags:        []string{"crawler"},
	}, func(ctx context.Context, input *crawler.GmapsAllDirectionsInput) (*crawler.GmapsAllDirectionsOutput, error) {
		if input.Body.Origin == "" {
			c.logger.Warn().Msg("缺少必要的 origin 參數")
			return nil, huma.Error400BadRequest("origin cannot be empty")
		}
		if input.Body.Destination == "" {
			c.logger.Warn().Msg("缺少必要的 destination 參數")
			return nil, huma.Error400BadRequest("destination cannot be empty")
		}

		routes, err := c.crawlerService.GetAllDirections(ctx, input.Body.Origin, input.Body.Destination)
		if err != nil {
			c.logger.Error().Err(err).Str("origin", input.Body.Origin).Str("destination", input.Body.Destination).Msg("兩點間所有路線規劃執行失敗")
			return nil, huma.Error500InternalServerError("Failed to get all directions")
		}

		resp := &crawler.GmapsAllDirectionsOutput{}
		resp.Body.Routes = routes
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "gmaps-directions",
		Method:      http.MethodPost,
		Path:        "/crawler/gmaps-directions",
		Summary:     "Google Maps 路線規劃",
		Tags:        []string{"crawler"},
	}, func(ctx context.Context, input *crawler.GmapsDirectionsInput) (*crawler.GmapsDirectionsOutput, error) {
		if input.Body.Start == "" || input.Body.End == "" {
			c.logger.Warn().Str("start", input.Body.Start).Str("end", input.Body.End).Msg("缺少必要的 start 或 end 參數")
			return nil, huma.Error400BadRequest("start and end cannot be empty")
		}

		routes, err := c.crawlerService.GetGoogleMapsDirections(ctx, input.Body.Start, input.Body.End)
		if err != nil {
			c.logger.Error().Err(err).Str("start", input.Body.Start).Str("end", input.Body.End).Msg("Google Maps 路線規劃執行失敗")
			return nil, huma.Error500InternalServerError("Failed to execute google maps directions search")
		}

		resp := &crawler.GmapsDirectionsOutput{
			Body: routes,
		}
		return resp, nil
	})
}

package infra

import (
	"net/http"
)

type GoogleConfig struct {
	APIKey string
}

type GoogleClient struct {
	APIKey     string
	HTTPClient *http.Client
}

func NewGoogleClient(config GoogleConfig) *GoogleClient {
	return &GoogleClient{
		APIKey:     config.APIKey,
		HTTPClient: &http.Client{},
	}
}

func (g *GoogleClient) BuildURL(base string, params map[string]string) string {
	url := base + "?key=" + g.APIKey
	for k, v := range params {
		url += "&" + k + "=" + v
	}
	return url
}

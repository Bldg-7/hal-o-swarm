package halctl

type CostSummary struct {
	Period       string  `json:"period"`
	TotalCost    float64 `json:"total_cost"`
	SessionCount int     `json:"session_count"`
	TokensUsed   int     `json:"tokens_used"`
}

func GetCostToday(client *HTTPClient) (*CostSummary, error) {
	body, err := client.Get("/api/v1/cost/today")
	if err != nil {
		return nil, err
	}

	var cost CostSummary
	if err := ParseResponse(body, &cost); err != nil {
		return nil, err
	}

	return &cost, nil
}

func GetCostWeek(client *HTTPClient) (*CostSummary, error) {
	body, err := client.Get("/api/v1/cost/week")
	if err != nil {
		return nil, err
	}

	var cost CostSummary
	if err := ParseResponse(body, &cost); err != nil {
		return nil, err
	}

	return &cost, nil
}

func GetCostMonth(client *HTTPClient) (*CostSummary, error) {
	body, err := client.Get("/api/v1/cost/month")
	if err != nil {
		return nil, err
	}

	var cost CostSummary
	if err := ParseResponse(body, &cost); err != nil {
		return nil, err
	}

	return &cost, nil
}

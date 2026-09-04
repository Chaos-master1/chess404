package main

import (
	"net/http"
	"time"
)

// Upstream health aggregation.

func collectGatewayStatus(config GatewayConfig, client *http.Client, r *http.Request) GatewaySystemStatus {
	services := map[string]GatewayServiceHealth{
		"match":       fetchGatewayJSON(r, client, config.MatchServiceURL+"/api/system/status"),
		"platform":    fetchGatewayJSON(r, client, config.PlatformServiceURL+"/api/platform/status"),
		"matchmaking": fetchGatewayJSON(r, client, config.MatchmakingServiceURL+"/api/status"),
	}

	status := "ok"
	for name := range services {
		if !services[name].Healthy {
			status = "degraded"
		}
		service := services[name]
		service.URL = ""
		service.Payload = nil
		service.Error = ""
		services[name] = service
	}

	return GatewaySystemStatus{
		Status:    status,
		Service:   "gateway",
		CheckedAt: time.Now().UTC(),
		Services:  services,
	}
}

package config

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/siddontang/go/log"
)

func CreatePrometheusClient() (api.Client, error) {
	config := api.Config{
		Address: os.Getenv("PROMETHEUS_ADDRESS"),
	}
	return api.NewClient(config)
}

func QueryPrometheus(client api.Client, query string) (model.Vector, error) {
	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, warnings, err := v1api.Query(ctx, query, time.Now())
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		log.Warn("Prometheus query warnings: ", warnings)
	}

	vector, ok := result.(model.Vector)
	if !ok {
		return nil, errors.New("unexpected result type from Prometheus")
	}

	return vector, nil
}

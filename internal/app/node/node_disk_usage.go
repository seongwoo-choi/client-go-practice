package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

type NodeDiskUsageType struct {
	NodeName  string
	DiskUsage float64
}

func NodeDiskUsage(clientSet *kubernetes.Clientset, percentage string) ([]NodeDiskUsageType, error) {
	query := fmt.Sprintf("(1 - node_filesystem_avail_bytes / node_filesystem_size_bytes) * 100 > %s", percentage)

	prometheusClient, err := createPrometheusClient()
	if err != nil {
		log.WithError(err).Error("Failed to create Prometheus client")
		return nil, err
	}

	result, err := queryPrometheus(prometheusClient, query)
	if err != nil {
		log.WithError(err).Error("Failed to query Prometheus")
		return nil, err
	}

	return parseResult(result), nil
}

func createPrometheusClient() (api.Client, error) {
	config := api.Config{
		Address: os.Getenv("PROMETHEUS_ADDRESS"),
	}
	return api.NewClient(config)
}

func queryPrometheus(client api.Client, query string) (model.Vector, error) {
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

func parseResult(vector model.Vector) []NodeDiskUsageType {
	var nodeDiskUsage []NodeDiskUsageType
	for _, sample := range vector {
		nodeName, diskUsage := extractNodeDiskUsage(sample)
		nodeDiskUsage = append(nodeDiskUsage, NodeDiskUsageType{
			NodeName:  nodeName,
			DiskUsage: diskUsage,
		})
	}
	return nodeDiskUsage
}

func extractNodeDiskUsage(sample *model.Sample) (string, float64) {
	nodeName := string(sample.Metric["instance"])
	nodeName = nodeName[0:strings.Index(nodeName, ":")]
	diskUsage, _ := strconv.ParseFloat(sample.Value.String(), 64)
	return nodeName, diskUsage
}

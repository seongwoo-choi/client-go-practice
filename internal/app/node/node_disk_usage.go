package node

import (
	"client-go/config"
	"fmt"
	"strconv"

	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

type nodeDiskUsageType struct {
	NodeName  string
	DiskUsage float64
}

func GetNodeDiskUsage(clientSet kubernetes.Interface, percentage string) ([]nodeDiskUsageType, error) {
	query := fmt.Sprintf("(1 - node_filesystem_avail_bytes / node_filesystem_size_bytes) * 100 > %s", percentage)

	prometheusClient, err := config.CreatePrometheusClient()
	if err != nil {
		log.WithError(err).Error("Failed to create Prometheus client")
		return nil, err
	}

	result, err := config.QueryPrometheus(prometheusClient, query)
	if err != nil {
		log.WithError(err).Error("Failed to query Prometheus")
		return nil, err
	}

	return parseResult(result), nil
}

func parseResult(vector model.Vector) []nodeDiskUsageType {
	var nodeDiskUsage []nodeDiskUsageType
	for _, sample := range vector {
		nodeName, diskUsage := extractNodeDiskUsage(sample)
		nodeDiskUsage = append(nodeDiskUsage, nodeDiskUsageType{
			NodeName:  nodeName,
			DiskUsage: diskUsage,
		})
	}
	return nodeDiskUsage
}

func extractNodeDiskUsage(sample *model.Sample) (string, float64) {
	nodeName := string(sample.Metric["node"])
	diskUsage, _ := strconv.ParseFloat(sample.Value.String(), 64)
	return nodeName, diskUsage
}

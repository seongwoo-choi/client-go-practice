package node

import (
	"client-go/config"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
)

type NodeDiskUsageType struct {
	NodeName  string
	DiskUsage float64
}

func GetNodeDiskUsage(clientSet kubernetes.Interface, percentage string) ([]NodeDiskUsageType, error) {
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

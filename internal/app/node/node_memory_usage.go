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

type NodeMemoryUsageType struct {
	NodeName    string
	MemoryUsage float64
}

func GetNodeMemoryUsage(clientSet kubernetes.Interface, percentage string) ([]NodeMemoryUsageType, error) {
	query := fmt.Sprintf("100 * (1 - (node_memory_MemFree_bytes + node_memory_Cached_bytes + node_memory_Buffers_bytes) / node_memory_MemTotal_bytes) < %s", percentage)

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

	return parseMemoryResult(result), nil
}

func parseMemoryResult(vector model.Vector) []NodeMemoryUsageType {
	var nodeMemoryUsage []NodeMemoryUsageType
	for _, sample := range vector {
		nodeName, memoryUsage := extractMemoryUsage(sample)
		nodeMemoryUsage = append(nodeMemoryUsage, NodeMemoryUsageType{
			NodeName:    nodeName,
			MemoryUsage: memoryUsage,
		})
	}
	return nodeMemoryUsage
}

func extractMemoryUsage(sample *model.Sample) (string, float64) {
	nodeName := string(sample.Metric["instance"])
	nodeName = nodeName[0:strings.Index(nodeName, ":")]
	memoryUsage, _ := strconv.ParseFloat(sample.Value.String(), 64)
	return nodeName, memoryUsage
}

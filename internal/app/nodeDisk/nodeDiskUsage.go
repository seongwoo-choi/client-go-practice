package nodeDiskUsage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/gofiber/fiber/v2/log"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type NodeDiskUsageType struct {
	NodeName  string
	DiskUsage float64
}

func NodeDiskUsage(clientSet *kubernetes.Clientset, percentage string) ([]NodeDiskUsageType, error) {
	var nodeDiskUsage []NodeDiskUsageType
	query := fmt.Sprintf("(1 - node_filesystem_avail_bytes / node_filesystem_size_bytes) * 100 > %s", percentage)
	prometheusClient, err := api.NewClient(api.Config{
		Address: os.Getenv("PROMETHEUS_ADDRESS"),
	})

	if err != nil {
		log.Error(err)
		return nil, err
	}

	v1api := v1.NewAPI(prometheusClient)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, warnings, err := v1api.Query(ctx, query, time.Now(), v1.WithTimeout(5*time.Second))
	if err != nil {
		log.Error(err)
		return nil, err
	}
	if len(warnings) > 0 {
		return nil, errors.New("warnings encountered during query")
	}

	vector, ok := result.(model.Vector)
	if !ok {
		return nil, errors.New("unexpected result type")
	}

	for _, sample := range vector {
		nodeName := string(sample.Metric["instance"])
		nodeName = nodeName[0:strings.Index(nodeName, ":")]
		diskUsage, _ := strconv.ParseFloat(sample.Value.String(), 64)
		nodeDiskUsage = append(nodeDiskUsage, NodeDiskUsageType{
			NodeName:  nodeName,
			DiskUsage: diskUsage,
		})
	}

	return nodeDiskUsage, nil
}

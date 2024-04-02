package nodeDiskUsage

import (
	"context"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func NodeDrain(clientSet *kubernetes.Clientset, percentage string) (any, error) {
	var drainNodeNames []string
	overNodes, err := NodeDiskUsage(clientSet, percentage)
	if err != nil {
		return nil, err
	}

	// log.Info("Draining nodes with disk usage over " + percentage + "%" + "... Drain Nodes: " + fmt.Sprintf("%v", overNodes))

	nodes, err := clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	log.Info("overNodes")

	for _, node := range nodes.Items {
		for _, overNode := range overNodes {
			if strings.Contains(node.Annotations["alpha.kubernetes.io/provided-node-ip"], overNode.NodeName) && node.Labels["karpenter.sh/provisioner-name"] == os.Getenv("DRAIN_NODE_LABELS") {
				log.Info("Node Name: "+node.Name, ", Labels: ", node.Labels["karpenter.sh/provisioner-name"])
				drainNodeNames = append(drainNodeNames, node.Name)
			}
		}
	}

	// 실제 Node Drain을 수행하는 부분

	return drainNodeNames, nil
}

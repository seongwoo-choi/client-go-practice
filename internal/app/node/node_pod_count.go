package node

import (
	"context"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type NodePodUsage struct {
	NodeName string
	PodCount int
}

func GetNodePodUsageByLabel(clientSet *kubernetes.Clientset) ([]NodePodUsage, error) {
	labelSelector := fmt.Sprintf("karpenter.sh/provisioner-name=%s", os.Getenv("DRAIN_NODE_LABELS_1"))
	nodes, err := clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}

	var nodePodUsages []NodePodUsage
	for _, node := range nodes.Items {
		pods, err := clientSet.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
			FieldSelector: "spec.nodeName=" + node.Name,
		})
		if err != nil {
			return nil, err
		}

		nodePodUsages = append(nodePodUsages, NodePodUsage{
			NodeName: node.Name,
			PodCount: len(pods.Items),
		})
	}

	return nodePodUsages, nil
}

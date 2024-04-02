package nodeDiskUsage

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func NodeDrain(clientSet *kubernetes.Clientset, percentage string) error {
	var drainNodeNames []string
	overNodes, err := NodeDiskUsage(clientSet, percentage)
	if err != nil {
		return err
	}

	// log.Info("Draining nodes with disk usage over " + percentage + "%" + "... Drain Nodes: " + fmt.Sprintf("%v", overNodes))

	nodes, err := clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, node := range nodes.Items {
		for _, overNode := range overNodes {
			if strings.Contains(node.Annotations["alpha.kubernetes.io/provided-node-ip"], overNode.NodeName) && node.Labels["karpenter.sh/provisioner-name"] == os.Getenv("DRAIN_NODE_LABELS") {
				log.Info("Node Name: "+node.Name, ", Labels: ", node.Labels["karpenter.sh/provisioner-name"])
				drainNodeNames = append(drainNodeNames, node.Name)
			}
		}
	}

	for _, nodeName := range drainNodeNames {
		log.Info("Draining node " + nodeName + "...")
		err := cordenNode(clientSet, nodeName)
		if err != nil {
			return err
		}
	}

	for _, nodeName := range drainNodeNames {
		log.Info("Evicting pods in node " + nodeName + "...")
		err := evictedPod(clientSet, nodeName)
		if err != nil {
			return err
		}
	}

	return nil
}

func cordenNode(clientSet *kubernetes.Clientset, nodeName string) error {
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	// 이미 스케줄링 불가능 상태라면 스킵
	if node.Spec.Unschedulable {
		return nil
	}

	log.Info("Corden node", nodeName)
	node.Spec.Unschedulable = true
	_, err = clientSet.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
	return err
}

func evictedPod(clientSet *kubernetes.Clientset, nodeName string) error {
	// 데몬셋, 스테이트풀셋, 시스템 파드를 제외한 모든 파드를 종료
	// 일단 위 과정을 생략하고 단순화하여 모든 파드를 조회
	log.Info("Listing pods in node", nodeName)

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			// 타임아웃 발생 시
			log.Info("Timeout reached while waiting for pods to be terminated in node", nodeName)
			return fmt.Errorf("timeout reached while evicting pods from node %s", nodeName)
		default:
			pods, err := clientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
				FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Succeeded,status.phase!=Failed", nodeName),
			})
			if err != nil {
				return err
			}

			if len(pods.Items) == 0 {
				// 모든 파드가 종료되었습니다.
				log.Info("All pods in node", nodeName, "have been successfully evicted.")
				return nil
			}

			// 파드 삭제 로직
			// 예: clientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{})
			log.Info("Deleted Pod Name: ", pods.Items[0].Name)

			log.Info("Waiting for pods to be terminated...")
			time.Sleep(10 * time.Second)
		}
	}
}

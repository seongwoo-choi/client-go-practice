package nodeDiskUsage

import (
	"context"
	"fmt"
	coreV1 "k8s.io/api/core/v1"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// node 무한 루프 => pdb 있는 deployment 댓수 강제 증가 + keda desired 변경 => replicas
// pdb 무력화했다가 다시 실행하도록 하는 방법..label 변경하는 방법
// 순단 나도 상관없으면 => kubelet 이 죽었다고 판단하게 할 수 있는 기능으로 사용..
// 운영에도 쓸 거면 pdb 걸려 있을 때 => 그냥 멈춰야 된다. / 개발 알파에서도 사용할거면 => pdb 걸려서 멈출 경우 pdb 잠시 끄고 드레인(labels 잠깐 변경한다던가..)

func NodeDrain(clientSet *kubernetes.Clientset, percentage string) error {
	var drainNodeNames []string
	overNodes, err := NodeDiskUsage(clientSet, percentage)
	if err != nil {
		log.WithError(err).Error("Failed to get node disk usage")
		return err
	}

	nodes, err := clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		log.WithError(err).Error("Failed to list nodes")
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

	// 각 노드 별로 파드 종료 작업을 실행
	for _, nodeName := range drainNodeNames {
		if err := drainSingleNode(clientSet, nodeName); err != nil {
			// 오류 로깅 후 다음 노드로 넘어감. 타임아웃 오류도 여기서 처리됨.
			return err
		}
	}

	return nil
}

// drainSingleNode 함수는 하나의 노드에 대해 cordon 및 파드 종료 작업을 수행
func drainSingleNode(clientSet *kubernetes.Clientset, nodeName string) error {
	log.Info("Draining node " + nodeName + "...")
	if err := cordonNode(clientSet, nodeName); err != nil {
		return err
	}

	log.Info("Evicting pods in node " + nodeName + "...")
	if err := evictedPod(clientSet, nodeName); err != nil {
		return err
	}

	// 노드의 Instance ID 를 조회
	instanceId, err := getNodeInstanceId(clientSet, nodeName)
	if err != nil {
		return err
	}
	log.Info("Instance ID: " + instanceId)
	if instanceId == "" {
		return fmt.Errorf("failed to get instance ID for node %s", nodeName)
	}

	// 인스턴스 ID 로 EC2 인스턴스를 종료
	log.Info("Terminating instance " + instanceId + "...")
	if err := terminateInstance(instanceId); err != nil {
		return err
	}

	return nil
}

func cordonNode(clientSet *kubernetes.Clientset, nodeName string) error {
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		log.WithError(err).Error("Failed to get node")
		return err
	}

	// 이미 스케줄링 불가능 상태라면 스킵
	if node.Spec.Unschedulable {
		return nil
	}

	log.Info("cordon node", nodeName)
	node.Spec.Unschedulable = true
	_, err = clientSet.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{})
	return err
}

func evictedPod(clientSet *kubernetes.Clientset, nodeName string) error {
	log.Info("Listing pods in node", nodeName)

	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	// 삭제 가능한 파드의 수를 추적
	var deletablePodCount int

	for {
		select {
		case <-ctx.Done():
			// 타임아웃 발생 시
			log.Error("Timeout reached while waiting for pods to be evicted from node", nodeName)
			return fmt.Errorf("timeout reached while evicting pods from node %s", nodeName)
		default:
			pods, err := clientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
				FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Succeeded,status.phase!=Failed", nodeName),
			})
			if err != nil {
				log.WithError(err).Error("Failed to list pods for eviction")
				return err
			}

			// 아래 반복문에서 관리되지 않는 파드들을 카운팅
			deletablePodCount = 0
			for _, pod := range pods.Items {
				if !isManagedByDaemonSetOrStatefulSet(pod) {
					deletablePodCount++
				}
			}

			// 관리되지 않는 파드가 없다면 종료
			if deletablePodCount == 0 {
				log.Info("All eligible pods in node", nodeName, "have been successfully evicted.")
				return nil
			}

			// 관리되지 않는 파드들에 대해서만 삭제를 시도
			for _, pod := range pods.Items {
				if !isManagedByDaemonSetOrStatefulSet(pod) {
					log.Infof("Deleting pod %s from node %s", pod.Name, nodeName)
					if err := clientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
						log.WithError(err).Error("Failed to delete pod")
						return err
					}
				}
			}

			log.Info("Waiting for eligible pods to be terminated...")
			time.Sleep(2 * time.Second)
		}
	}
}

// isManagedByDaemonSetOrStatefulSet checks if the pod is managed by a DaemonSet or StatefulSet
func isManagedByDaemonSetOrStatefulSet(pod coreV1.Pod) bool {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "DaemonSet" || ref.Kind == "StatefulSet" {
			return true
		}
	}
	return false
}

// 인스턴스 ID 로 EC2 인스턴스를 종료
func terminateInstance(instanceId string) error {
	// 인스턴스 종료 로직
	// sess := session.Must(session.NewSession())
	// svc := ec2.New(sess)
	// _, err := svc.TerminateInstances(&ec2.TerminateInstancesInput{
	// 	InstanceIds: []*string{&instanceId},
	// })
	// return err
	return nil
}

func getNodeInstanceId(clientSet *kubernetes.Clientset, nodeName string) (string, error) {
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, addr := range node.Status.Addresses {
		if addr.Type == "InternalIP" {
			log.Info("Found internal IP: ", addr.Address)
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("failed to get internal IP for node %s", nodeName)
}

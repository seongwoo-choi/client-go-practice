package nodeDiskUsage

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
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
				log.Info("Node Name: ", node.Name, ", instance type: ", node.Labels["beta.kubernetes.io/instance-type"], ", provisioner name: ", node.Labels["karpenter.sh/provisioner-name"])
				if err := drainSingleNode(clientSet, node.Name); err != nil {
					return fmt.Errorf("failed to drain node ", node.Name, ", instance type: ", node.Labels["beta.kubernetes.io/instance-type"], ", provisioner name: ", node.Labels["karpenter.sh/provisioner-name"])
				}
			}
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

	if err := evictPods(clientSet, nodeName); err != nil {
		return err
	}

	// 노드의 Instance ID 를 조회
	instanceId, err := getNodeInstanceId(clientSet, nodeName)
	if err != nil {
		return err
	}
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
		log.Info("Node ", nodeName, " is already unschedulable")
		return nil
	}

	log.Info("Cordoning node ", nodeName)
	node.Spec.Unschedulable = true
	if _, err = clientSet.CoreV1().Nodes().Update(context.Background(), node, metav1.UpdateOptions{}); err != nil {
		return err
	}

	return nil
}

func evictPods(clientSet *kubernetes.Clientset, nodeName string) error {
	log.Info("Evicting pods in node ", nodeName)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	gracePeriod := int64(30)
	propagationPolicy := metav1.DeletePropagationOrphan // 파드 삭제 시 관련된 리소스를 삭제하지 않음

	for {
		pods, err := clientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
		})
		if err != nil {
			return err
		}

		// 삭제 가능한 파드의 수를 추적
		deletablePodCount := 0
		for _, pod := range pods.Items {
			if !isManagedByDaemonSetOrStatefulSet(pod) {
				deletablePodCount++
			}
		}

		// 관리되지 않는 파드가 없다면 종료
		if deletablePodCount == 0 {
			log.Info("All eligible pods in node ", nodeName, " have been successfully evicted.")
			return nil
		}

		// 관리되지 않는 파드들에 대해서만 삭제를 시도
		for _, pod := range pods.Items {
			if !isManagedByDaemonSetOrStatefulSet(pod) {
				log.Infof("Deleting pod %s from node %s", pod.Name, nodeName)
				if err := clientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
					GracePeriodSeconds: &gracePeriod,
					PropagationPolicy:  &propagationPolicy,
				}); err != nil {
					return fmt.Errorf("failed to delete pod %s from node %s", pod.Name, nodeName)
				}
			}
		}

		log.Info("Waiting for pods to be terminated...")
		time.Sleep(2 * time.Second)
	}
}

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
	// ec2 인스턴스 종료 로직
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("ap-northeast-2"),
	})
	if err != nil {
		return err
	}

	ec2Svc := ec2.New(sess)
	_, err = ec2Svc.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: []*string{aws.String(instanceId)},
	})
	if err != nil {
		return err
	}

	log.Info("Successfully terminated instance ", instanceId)
	return nil
}

func getNodeInstanceId(clientSet *kubernetes.Clientset, nodeName string) (string, error) {
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, addr := range node.Status.Addresses {
		if addr.Type == "InternalIP" {
			return addr.Address, nil
		}
	}
	return "", fmt.Errorf("failed to get internal IP for node %s", nodeName)
}

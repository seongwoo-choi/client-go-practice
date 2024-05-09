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
// GracePeriodSeconds 를 0 으로 설정하여 파드를 즉시 삭제하도록 요청할 수 있다.

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

	gracePeriod := int64(30) // 일반 삭제 시 유예 기간
	immediate := int64(0)    // 강제 삭제 시 유예 기간
	propagationPolicy := metav1.DeletePropagationOrphan

	for {
		pods, err := clientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
		})
		if err != nil {
			return fmt.Errorf("failed to list pods in node %s", nodeName)
		}

		if len(pods.Items) == 0 {
			log.Info("All pods in node ", nodeName, " have been successfully evicted.")
			return nil
		}

		for _, pod := range pods.Items {
			if !isManagedByDaemonSetOrStatefulSet(pod) {
				grace := &gracePeriod
				if shouldForceDelete(pod) {
					grace = &immediate
				}
				log.Infof("Deleting pod %s from node %s with grace period of %d seconds", pod.Name, nodeName, *grace)
				if err := clientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
					GracePeriodSeconds: grace,
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

func shouldForceDelete(pod coreV1.Pod) bool {
	// pod 가 pdb 에 의해 막혔거나 KEDA 에 의해 제어되는지 확인
	for _, cond := range pod.Status.Conditions {
		if cond.Type == coreV1.PodScheduled && cond.Status == coreV1.ConditionFalse && cond.Reason == "Unschedulable" {
			return true
		}
	}
	return false
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

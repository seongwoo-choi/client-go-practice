package node

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"

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
			provisionerName := node.Labels["karpenter.sh/provisioner-name"]
			if strings.Contains(node.Annotations["alpha.kubernetes.io/provided-node-ip"], overNode.NodeName) && (provisionerName == os.Getenv("DRAIN_NODE_LABELS_1") || provisionerName == os.Getenv("DRAIN_NODE_LABELS_2")) {
				log.Info("Node Name: ", node.Name, ", instance type: ", node.Labels["beta.kubernetes.io/instance-type"], ", provisioner name: ", provisionerName)
				if err := drainSingleNode(clientSet, node.Name); err != nil {
					log.Info("failed to drain node ", node.Name, ", instance type: ", node.Labels["beta.kubernetes.io/instance-type"], ", provisioner name: ", provisionerName)
					return err
				}
			}
		}
	}

	return nil
}

// drainSingleNode 함수는 하나의 노드에 대해 cordon 및 파드 종료 작업을 수행
func drainSingleNode(clientSet *kubernetes.Clientset, nodeName string) error {
	instanceId, err := getNodeInstanceId(clientSet, nodeName)
	if err != nil {
		return fmt.Errorf("failed to get instance ID for node %s: %w", nodeName, err)
	}

	if err := cordonNode(clientSet, nodeName); err != nil {
		return fmt.Errorf("failed to cordon node %s: %w", nodeName, err)
	}

	if err := evictPods(clientSet, nodeName); err != nil {
		return fmt.Errorf("failed to evict pods from node %s: %w", nodeName, err)
	}

	if err := waitForPodsToTerminate(clientSet, nodeName); err != nil {
		return fmt.Errorf("failed to wait for pods to terminate on node %s: %w", nodeName, err)
	}

	log.Infof("Terminating instance %s for node %s", instanceId, nodeName)
	if err := terminateInstance(instanceId); err != nil {
		return fmt.Errorf("failed to terminate instance %s: %w", instanceId, err)
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

	pods, err := getNonCriticalPods(clientSet, nodeName)
	if err != nil {
		return fmt.Errorf("failed to get non-critical pods for eviction from node %s: %v", nodeName, err)
	}

	for _, pod := range pods {
		grace := &gracePeriod
		if shouldForceDelete(pod) {
			grace = &immediate
		}

		log.Infof("Attempting to delete pod %s from node %s with a grace period of %d seconds", pod.Name, nodeName, *grace)
		err := clientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
			GracePeriodSeconds: grace,
			PropagationPolicy:  &propagationPolicy,
		})

		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof("Pod %s already deleted from node %s", pod.Name, nodeName)
				continue
			}
			return fmt.Errorf("failed to delete pod %s from node %s: %v", pod.Name, nodeName, err)
		}
	}

	log.Infof("Completed evicting pods from node %s", nodeName)
	return nil
}

func waitForPodsToTerminate(clientSet kubernetes.Interface, nodeName string) error {
	log.Infof("Waiting for all non-critical pods to terminate on node %s", nodeName)

	_, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	for {
		pods, err := getNonCriticalPods(clientSet, nodeName)
		if err != nil {
			return fmt.Errorf("failed to get non-critical pods for eviction from node %s: %v", nodeName, err)
		}

		if len(pods) == 0 {
			log.Infof("All non-critical pods have been terminated on node %s", nodeName)
			return nil
		}

		log.Infof("Still waiting for %d pods to terminate on node %s", len(pods), nodeName)
		time.Sleep(5 * time.Second)
	}
}

func getNonCriticalPods(clientSet kubernetes.Interface, nodeName string) ([]coreV1.Pod, error) {
	podList, err := clientSet.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s,status.phase!=Succeeded,status.phase!=Failed", nodeName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %v", nodeName, err)
	}

	var pods []coreV1.Pod
	for _, pod := range podList.Items {
		if !isManagedByDaemonSetOrStatefulSet(pod) {
			if !isCriticalPod(pod) {
				pods = append(pods, pod)
			}
		}
	}

	return pods, nil
}

func isCriticalPod(pod coreV1.Pod) bool {
	if pod.Namespace == "kube-system" {
		return true
	}

	if _, exists := pod.Annotations["scheduler.alpha.kubernetes.io/critical-pod"]; exists {
		return true
	}

	return false
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

func getNodeInstanceId(clientSet kubernetes.Interface, nodeName string) (string, error) {
	node, err := clientSet.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil {
		log.WithError(err).Errorf("Failed to get node %s", nodeName)
		return "", err
	}

	// 로깅으로 메타데이터 확인
	log.Infof("Node %s providerID: %s", nodeName, node.Spec.ProviderID)

	// EC2 인스턴스 ID 추출
	if strings.HasPrefix(node.Spec.ProviderID, "aws:///") {
		splitId := strings.Split(node.Spec.ProviderID, "/")
		if len(splitId) >= 5 {
			instanceId := splitId[4]
			return instanceId, nil
		}
	}

	return "", fmt.Errorf("failed to parse instance ID from node %s: providerID %s is malformed", nodeName, node.Spec.ProviderID)
}

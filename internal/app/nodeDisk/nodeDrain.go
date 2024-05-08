package nodeDiskUsage

import (
	"context"
	"fmt"
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
	// 데몬셋, 스테이트풀셋, 시스템 파드를 제외한 모든 파드를 종료
	// 일단 위 과정을 생략하고 단순화하여 모든 파드를 조회
	log.Info("Listing pods in node", nodeName)

	// todo: 적절한 타임아웃 설정 필요(노드에서 파드 삭제할 때)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			// 타임아웃 발생 시
			// 사람이 결정..? slack 남기던가 로그를 남기는 방식으로
			// pod describe 사용해서 왜 타임아웃이 발생했는지 이벤트를 확인해야 된다.
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

			// todo:파드 삭제 로직 추가
			// clientSet.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Items[0].Name, metav1.DeleteOptions{})
			log.Info("Deleted Pod Name: ", pods.Items[0].Name)

			log.Info("Waiting for pods to be terminated...")
			time.Sleep(10 * time.Second)
		}
	}
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

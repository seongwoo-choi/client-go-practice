package checking_deployment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	appV1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type DeploymentContainer struct {
	DeploymentName        string
	OldContainerImageName string
	NewContainerImageName string
	Namespace             string
	UpdatedTime           time.Time
}

func sendSlackNotification(message, slackWebhookURL string) error {
	payload := map[string]interface{}{
		"text": "Deployment Update Notification",
		"blocks": []map[string]interface{}{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": message,
				},
			},
		},
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(slackWebhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack notification returned status code %d", resp.StatusCode)
	}

	return nil
}

func formatChanges(oldDep, newDep *appV1.Deployment) string {
	var changes []string
	if oldDep.Name != newDep.Name {
		changes = append(changes, fmt.Sprintf("- Name changed from `%s` to `%s`", oldDep.Name, newDep.Name))
	}
	// oldDep 와 newDep 객체가 동등한지 체크
	if !reflect.DeepEqual(oldDep.Spec.Template.Spec.Containers, newDep.Spec.Template.Spec.Containers) {
		changes = append(changes, "- Container specs have changed:")
		changes = append(changes, detectContainerChanges(oldDep.Spec.Template.Spec.Containers, newDep.Spec.Template.Spec.Containers)...)
		//changes = append(changes, detectDetailedChanges(oldDep, newDep)...)
	}
	labelChanges := detectLabelChanges(oldDep.Labels, newDep.Labels)
	if len(labelChanges) > 0 {
		changes = append(changes, "- Label changes:")
		changes = append(changes, labelChanges...)
	}
	return strings.Join(changes, "\n")
}

//func detectDetailedChanges(oldDep, newDep *appV1.Deployment) []string {
//	var changes []string
//
//	// 컨테이너 수 비교
//	if len(oldDep.Spec.Template.Spec.Containers) != len(newDep.Spec.Template.Spec.Containers) {
//		changes = append(changes, fmt.Sprintf("Number of containers changed from `%d` to `%d`",
//			len(oldDep.Spec.Template.Spec.Containers), len(newDep.Spec.Template.Spec.Containers)))
//	}
//
//	// 컨테이너별 세부 비교
//	for i, oldContainer := range oldDep.Spec.Template.Spec.Containers {
//		if i >= len(newDep.Spec.Template.Spec.Containers) {
//			continue
//		}
//		newContainer := newDep.Spec.Template.Spec.Containers[i]
//
//		if oldContainer.Image != newContainer.Image {
//			changes = append(changes, fmt.Sprintf("Container `%s` image changed from `%s` to `%s`",
//				oldContainer.Name, oldContainer.Image, newContainer.Image))
//		}
//
//		if !reflect.DeepEqual(oldContainer.Resources, newContainer.Resources) {
//			changes = append(changes, fmt.Sprintf("Resources for container `%s` changed", oldContainer.Name))
//		}
//
//		// 다른 필드들도 비슷하게 추가 가능
//	}
//
//	return changes
//}

func detectContainerChanges(oldContainers, newContainers []coreV1.Container) []string {
	var changes []string
	for i, newContainer := range newContainers {
		if i < len(oldContainers) {
			oldContainer := oldContainers[i]
			if oldContainer.Image != newContainer.Image {
				changes = append(changes, fmt.Sprintf("    - Image changed from `%s` to `%s`", oldContainer.Image, newContainer.Image))
			}
			if !reflect.DeepEqual(oldContainer.Resources, newContainer.Resources) {
				changes = append(changes, fmt.Sprintf("    - Resources changed for container `%s`", newContainer.Name))
			}
		} else {
			changes = append(changes, fmt.Sprintf("    - New container added: `%s`", newContainer.Name))
		}
	}
	return changes
}

func detectLabelChanges(oldLabels, newLabels map[string]string) []string {
	var changes []string
	// 새로 추가된 레이블 검사
	for key, newVal := range newLabels {
		if oldVal, ok := oldLabels[key]; !ok {
			changes = append(changes, fmt.Sprintf("    - New label added: `%s=%s`", key, newVal))
		} else if newVal != oldVal {
			changes = append(changes, fmt.Sprintf("    - Label `%s` changed from `%s` to `%s`", key, oldVal, newVal))
		}
	}
	// 삭제된 레이블 검사
	for key, oldVal := range oldLabels {
		if _, ok := newLabels[key]; !ok {
			changes = append(changes, fmt.Sprintf("    - Label `%s=%s` removed", key, oldVal))
		}
	}
	return changes
}

func handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDep, _ := oldObj.(*appV1.Deployment)
	newDep, _ := newObj.(*appV1.Deployment)

	webhookURL := os.Getenv("SLACK_WEBHOOK_URL")
	message := formatChanges(oldDep, newDep)
	if message != "" {
		message = fmt.Sprintf("*Updated Deployment:* `%s`\n%s", newDep.Name, message)
		if err := sendSlackNotification(message, webhookURL); err != nil {
			log.WithError(err).Error("Failed to send Slack notification")
		}
	}
}

func CheckingContainerImage(clientSet *kubernetes.Clientset) {
	factory := informers.NewSharedInformerFactory(clientSet, time.Second*30)
	informer := factory.Apps().V1().Deployments().Informer()

	stopCh := make(chan struct{})
	defer close(stopCh)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: handleDeploymentUpdate,
	})

	go informer.Run(stopCh)
	<-stopCh
}

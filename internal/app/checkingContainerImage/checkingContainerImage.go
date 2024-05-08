package checkingContainerImage

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"time"

	"encoding/json"
	log "github.com/sirupsen/logrus" // Updated to use Logrus for better logging capabilities
	appV1 "k8s.io/api/apps/v1"
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

func sendSlackNotification(message string) error {
	webhookURL := os.Getenv("SLACK_WEBHOOK_URL")
	payload := map[string]string{"text": message}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack notification returned status code %d", resp.StatusCode)
	}

	return nil
}

// findNewContainers identifies new containers in the new deployment not present in the old deployment
func findNewContainers(oldDep, newDep *appV1.Deployment) map[string]string {
	newImages := make(map[string]string)
	for _, newContainer := range newDep.Spec.Template.Spec.Containers {
		found := false
		for _, oldContainer := range oldDep.Spec.Template.Spec.Containers {
			if newContainer.Image == oldContainer.Image {
				found = true
				break
			}
		}
		if !found {
			newImages[newContainer.Image] = newContainer.Image
		}
	}
	return newImages
}

// handleDeploymentUpdate handles different scenarios when a deployment is updated
func handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDep, _ := oldObj.(*appV1.Deployment)
	newDep, _ := newObj.(*appV1.Deployment)

	newContainerCount := len(newDep.Spec.Template.Spec.Containers)
	oldContainerCount := len(oldDep.Spec.Template.Spec.Containers)

	containerUpdate := DeploymentContainer{
		DeploymentName: newDep.Name,
		Namespace:      newDep.Namespace,
		UpdatedTime:    time.Now().UTC(),
	}

	var message string
	switch {
	case newContainerCount > oldContainerCount:
		log.Infof("%s Deployment Container Added", newDep.Name)
		changedImages := findNewContainers(oldDep, newDep)
		for _, image := range changedImages {
			log.Infof("Added Container Image: %s", image)
			message += fmt.Sprintf("Added Container Image: %s\n", image)
		}
	case newContainerCount < oldContainerCount:
		log.Infof("%s Deployment Container Deleted", oldDep.Name)
		changedImages := findNewContainers(newDep, oldDep)
		for _, image := range changedImages {
			log.Infof("Deleted Container Image: %s", image)
			message += fmt.Sprintf("Deleted Container Image: %s\n", image)
		}
	case newContainerCount == oldContainerCount:
		for i := range newDep.Spec.Template.Spec.Containers {
			if newDep.Spec.Template.Spec.Containers[i].Image != oldDep.Spec.Template.Spec.Containers[i].Image {
				containerUpdate.OldContainerImageName = oldDep.Spec.Template.Spec.Containers[i].Image
				containerUpdate.NewContainerImageName = newDep.Spec.Template.Spec.Containers[i].Image
				log.Infof("Container Image Updated: %s to %s in Deployment %s", containerUpdate.OldContainerImageName, containerUpdate.NewContainerImageName, containerUpdate.DeploymentName)
				message += fmt.Sprintf("Container Image Updated: %s to %s in Deployment %s", containerUpdate.OldContainerImageName, containerUpdate.NewContainerImageName, containerUpdate.DeploymentName)
			}
		}
	}

	if message != "" {
		if err := sendSlackNotification(message); err != nil {
			log.WithError(err).Error("Failed to send Slack notification")
		}
	}
}

// CheckingContainerImage sets up an informer to monitor deployment changes
func CheckingContainerImage(clientSet *kubernetes.Clientset) {
	factory := informers.NewSharedInformerFactory(clientSet, time.Second*30)
	informer := factory.Apps().V1().Deployments().Informer()

	stopCh := make(chan struct{})
	defer close(stopCh)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: handleDeploymentUpdate,
	})

	go informer.Run(stopCh)

	// Block this goroutine indefinitely
	<-stopCh
}

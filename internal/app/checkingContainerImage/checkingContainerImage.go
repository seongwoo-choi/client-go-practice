package checkingContainerImage

import (
	"time"

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

	switch {
	case newContainerCount > oldContainerCount:
		log.Infof("%s Deployment Container Added", newDep.Name)
		changedImages := findNewContainers(oldDep, newDep)
		for _, image := range changedImages {
			log.Infof("Added Container Image: %s", image)
		}
	case newContainerCount < oldContainerCount:
		log.Infof("%s Deployment Container Deleted", oldDep.Name)
		changedImages := findNewContainers(newDep, oldDep)
		for _, image := range changedImages {
			log.Infof("Deleted Container Image: %s", image)
		}
	case newContainerCount == oldContainerCount:
		for i := range newDep.Spec.Template.Spec.Containers {
			if newDep.Spec.Template.Spec.Containers[i].Image != oldDep.Spec.Template.Spec.Containers[i].Image {
				containerUpdate.OldContainerImageName = oldDep.Spec.Template.Spec.Containers[i].Image
				containerUpdate.NewContainerImageName = newDep.Spec.Template.Spec.Containers[i].Image
				log.Infof("Container Image Updated: %s to %s in Deployment %s", containerUpdate.OldContainerImageName, containerUpdate.NewContainerImageName, containerUpdate.DeploymentName)
			}
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

package checkingContainerImage

import (
	"time"

	"github.com/gofiber/fiber/v3/log"
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

func addContainerImage(deploymentA *appV1.Deployment, deploymentB *appV1.Deployment, ca map[string]string) map[string]string {
	for i := 0; i < len(deploymentB.Spec.Template.Spec.Containers); i++ {
		c := deploymentB.Spec.Template.Spec.Containers[i].Image
		found := false

		for j := 0; j < len(deploymentA.Spec.Template.Spec.Containers); j++ {
			if c == deploymentA.Spec.Template.Spec.Containers[j].Image {
				found = true
				break
			}
		}

		if !found {
			ca[c] = c
		}
	}

	return ca
}

func CheckingContainerImage(clientSet *kubernetes.Clientset) {
	var deploymentContainer DeploymentContainer
	factory := informers.NewSharedInformerFactory(clientSet, time.Second*30)
	Informer := factory.Apps().V1().Deployments().Informer()

	stopCh := make(chan struct{})
	defer close(stopCh)

	Informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			deploymentOld, _ := oldObj.(*appV1.Deployment)
			deploymentNew, _ := newObj.(*appV1.Deployment)
			deploymentNewContainerLength := len(deploymentNew.Spec.Template.Spec.Containers)
			deploymentOldContainerLength := len(deploymentOld.Spec.Template.Spec.Containers)
			ca := map[string]string{}

			// 컨테이너 삭제 시 deploymentNew 의 컨테이너 갯수가 deploymentOld 보다 적다.
			// 컨테이너 추가 시 deploymentNew 의 컨테이너 갯수가 deploymentOld 보다 많다.
			// 만약 deploymentOld 와 deploymentNew 의 container 갯수가 다르면 새롭게 추가된 container 이므로 해당 container 의 정보를 출력한다.
			if deploymentNewContainerLength > deploymentOldContainerLength {
				// 컨테이너 추가 시
				log.Info("%s Deployment Container Added\n", deploymentNew.Name)

				addContainerImage(deploymentOld, deploymentNew, ca)
				deploymentContainer.DeploymentName = deploymentNew.Name
				deploymentContainer.Namespace = deploymentNew.Namespace
				deploymentContainer.UpdatedTime = time.Now().UTC()
				log.Info(deploymentContainer)

				for _, v := range ca {
					log.Info("Added Container Name: " + v)
				}

				// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송
			} else if deploymentNewContainerLength < deploymentOldContainerLength {
				// 컨테이너 삭제 시
				log.Info("%s Deployment Container Deleted\n", deploymentOld.Name)

				addContainerImage(deploymentNew, deploymentOld, ca)
				deploymentContainer.DeploymentName = deploymentOld.Name
				deploymentContainer.Namespace = deploymentOld.Namespace
				deploymentContainer.UpdatedTime = time.Now().UTC()
				log.Info(deploymentContainer)

				for _, v := range ca {
					log.Info("Deleted Container Name: " + v)
				}
				// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송

			} else if deploymentNewContainerLength == deploymentOldContainerLength {
				for i := 0; i < deploymentNewContainerLength; i++ {
					if deploymentOld.Spec.Template.Spec.Containers[i].Image != deploymentNew.Spec.Template.Spec.Containers[i].Image {
						deploymentContainer.DeploymentName = deploymentNew.Name
						deploymentContainer.OldContainerImageName = deploymentOld.Spec.Template.Spec.Containers[i].Image
						deploymentContainer.NewContainerImageName = deploymentNew.Spec.Template.Spec.Containers[i].Image
						deploymentContainer.Namespace = deploymentNew.Namespace
						deploymentContainer.UpdatedTime = time.Now().UTC()
						log.Info(deploymentContainer)
						// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송
					}
				}
			} else {
				deploymentContainer.DeploymentName = deploymentNew.Name
				deploymentContainer.Namespace = deploymentNew.Namespace
				deploymentContainer.UpdatedTime = time.Now().UTC()
				log.Error(deploymentContainer)

				// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송
			}
		},
	})

	go Informer.Run(stopCh)

	select {}
}

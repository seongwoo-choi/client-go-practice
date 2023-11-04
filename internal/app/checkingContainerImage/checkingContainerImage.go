package checkingContainerImage

import (
	"fmt"
	appV1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"time"
)

func CheckingContainerImage(clientSet *kubernetes.Clientset) {
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

			// 컨테이너 삭제 시 => deploymentOld 와 deploymentNew 의 container 갯수가 다르다.
			// deploymentNew 의 컨테이너 갯수가 deploymentOld 보다 적다.
			// 컨테이너 추가 시 => deploymentOld 와 deploymentNew 의 container 갯수가 다르다.
			// deploymentNew 의 컨테이너 갯수가 deploymentOld 보다 많다.

			// 만약 deploymentOld 와 deploymentNew 의 container 갯수가 다르면 새롭게 추가된 container 이므로 해당 container 의 정보를 출력한다.
			if deploymentNewContainerLength > deploymentOldContainerLength {
				// 컨테이너 추가 시

				fmt.Printf("%s Deployment Container Added\n", deploymentNew.Name)
				fmt.Printf("Deployment Namespace: %s\n", deploymentNew.Namespace)
				fmt.Printf("Updated Deployment Time: %s\n", time.Now().UTC())

				// 추가 된 container 의 이름을 map 에 저장
				for i := 0; i < deploymentNewContainerLength; i++ {
					c := deploymentNew.Spec.Template.Spec.Containers[i].Image
					for j := 0; j < deploymentOldContainerLength; j++ {
						if c == deploymentOld.Spec.Template.Spec.Containers[j].Image {
							break
						} else if j == deploymentOldContainerLength-1 {
							ca[c] = c
						}
					}
				}
				for _, v := range ca {
					fmt.Printf("Added Container Name: %s\n", v)
				}

				// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송
			} else if deploymentNewContainerLength < deploymentOldContainerLength {
				// 컨테이너 삭제 시
				fmt.Printf("%s Deployment Container Deleted\n", deploymentOld.Name)
				fmt.Printf("Deployment Namespace: %s\n", deploymentOld.Namespace)
				fmt.Printf("Updated Deployment Time: %s\n", time.Now().UTC())

				// 삭제 된 container 를 map 에 저장
				for i := 0; i < deploymentOldContainerLength; i++ {
					c := deploymentOld.Spec.Template.Spec.Containers[i].Image
					for j := 0; j < deploymentNewContainerLength; j++ {
						if c == deploymentNew.Spec.Template.Spec.Containers[j].Image {
							break
						} else if j == deploymentNewContainerLength-1 {
							ca[c] = c
						}
					}
				}
				for _, v := range ca {
					fmt.Printf("Deleted Container Name: %s\n", v)
				}
				// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송

			} else if deploymentNewContainerLength == deploymentOldContainerLength {
				for i := 0; i < deploymentNewContainerLength; i++ {
					if deploymentOld.Spec.Template.Spec.Containers[i].Image != deploymentNew.Spec.Template.Spec.Containers[i].Image {
						fmt.Printf("%s Deployment Container Image Updated\n", deploymentNew.Name)
						fmt.Printf("Change Container Image: %s =====>>> %s\n", deploymentOld.Spec.Template.Spec.Containers[i].Image, deploymentNew.Spec.Template.Spec.Containers[i].Image)
						fmt.Printf("Deployment Namespace: %s\n", deploymentNew.Namespace)
						fmt.Printf("Updated Deployment Time: %s\n", time.Now().UTC())

						// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송
					}
				}
			} else {
				fmt.Printf("Something Wrong\n")
				fmt.Printf("Deployment Name: %s\n", deploymentNew.Name)
				fmt.Printf("Deployment Namespace: %s\n", deploymentNew.Namespace)
				fmt.Printf("Updated Deployment Time: %s\n", time.Now().UTC())

				// datadog metric 으로 전송(prism2 api 호출) or slack 으로 전송
			}
		},
	})

	go Informer.Run(stopCh)

	select {}
}

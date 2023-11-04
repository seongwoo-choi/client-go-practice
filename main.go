package main

import (
	"flag"
	"fmt"
	appV1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	"time"
)

func main() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, configErr := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if configErr != nil {
		panic(configErr.Error())
	}

	// create the clientSet
	clientSet, clientSdtErr := kubernetes.NewForConfig(config)
	if clientSdtErr != nil {
		panic(clientSdtErr.Error())
	}

	factory := informers.NewSharedInformerFactory(clientSet, time.Second*30)
	informer := factory.Apps().V1().Deployments().Informer()

	stopCh := make(chan struct{})
	defer close(stopCh)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			deploymentOld, _ := oldObj.(*appV1.Deployment)
			deploymentNew, _ := newObj.(*appV1.Deployment)
			deploymentNewContainerLength := len(deploymentNew.Spec.Template.Spec.Containers)
			deploymentOldContainerLength := len(deploymentOld.Spec.Template.Spec.Containers)

			// 만약 deploymentOld 와 deploymentNew 의 container 갯수가 다르면 새롭게 추가된 container 이므로 해당 container 의 정보를 출력한다.
			if deploymentNewContainerLength > deploymentOldContainerLength {
				fmt.Printf("%s Deployment Container Added\n", deploymentNew.Name)
				fmt.Printf("Deployment Namespace: %s\n", deploymentNew.Namespace)
				fmt.Printf("Deployment CreationTime: %s\n", deploymentOld.CreationTimestamp)
				fmt.Printf("Updated Deployment Time: %s\n", time.Now().UTC())
				for i := 0; i < deploymentNewContainerLength; i++ {
					if deploymentOld.Spec.Template.Spec.Containers[i].Name != deploymentNew.Spec.Template.Spec.Containers[i].Name {
						fmt.Printf("Added Container Name: %s\n", deploymentNew.Spec.Template.Spec.Containers[i].Name)
						// datadog metric 으로 전송(prism2 api 호출)
					}
				}
				// datadog metric 으로 전송(prism2 api 호출)
			} else if deploymentNewContainerLength < deploymentOldContainerLength {
				fmt.Printf("%s Deployment Container Deleted\n", deploymentOld.Name)
				fmt.Printf("Deployment Namespace: %s\n", deploymentOld.Namespace)
				fmt.Printf("Deployment CreationTime: %s\n", deploymentOld.CreationTimestamp)
				fmt.Printf("Updated Deployment Time: %s\n", time.Now().UTC())
				for i := 0; i < deploymentOldContainerLength; i++ {
					if deploymentOld.Spec.Template.Spec.Containers[i].Name != deploymentNew.Spec.Template.Spec.Containers[i].Name {
						fmt.Printf("Deleted Container Name: %s\n", deploymentOld.Spec.Template.Spec.Containers[i].Name)
						// datadog metric 으로 전송(prism2 api 호출)
					}
				}
				// datadog metric 으로 전송(prism2 api 호출)
			} else {
				for i := 0; i < deploymentNewContainerLength; i++ {
					if deploymentOld.Spec.Template.Spec.Containers[i].Image != deploymentNew.Spec.Template.Spec.Containers[i].Image {
						fmt.Printf("%s Deployment Image Updated\n", deploymentNew.Name)
						fmt.Printf("Old Deployment Image: %s\n", deploymentOld.Spec.Template.Spec.Containers[i].Image)
						fmt.Printf("New Deployment Image: %s\n", deploymentNew.Spec.Template.Spec.Containers[i].Image)
						fmt.Printf("Deployment Namespace: %s\n", deploymentNew.Namespace)
						fmt.Printf("Deployment CreationTime: %s\n", deploymentOld.CreationTimestamp)
						fmt.Printf("Updated Deployment Time: %s\n", time.Now().UTC())
						// datadog metric 으로 전송(prism2 api 호출)
					}
				}
			}
		},
	})

	go informer.Run(stopCh)

	// Wait forever
	select {}
}

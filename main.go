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
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// create the clientSet
	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	factory := informers.NewSharedInformerFactory(clientSet, time.Second*30)
	informer := factory.Apps().V1().Deployments().Informer()

	stopCh := make(chan struct{})
	defer close(stopCh)

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			deploymentOld, _ := oldObj.(*appV1.Deployment)
			deploymentNew, _ := newObj.(*appV1.Deployment)
			for i := 0; i < len(deploymentNew.Spec.Template.Spec.Containers); i++ {
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
		},
	})

	go informer.Run(stopCh)

	// Wait forever
	select {}
}

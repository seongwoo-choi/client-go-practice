package main

import (
	"client-go/internal/app/checkingContainerImage"
	"flag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)

func main() {
	var kubeconfig *string

	// local 에서 실행 시 config 를 가져올 때 사용
	// kubeconfig 경로를 지정하지 않으면 $HOME/.kube/config 로 지정
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// 클러스터 내부에서 config 를 가져올 때 사용
	// config, err := rest.InClusterConfig()
	// if err != nil {
	//		panic(err.Error())
	// }

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

	checkingContainerImage.CheckingContainerImage(clientSet)
}

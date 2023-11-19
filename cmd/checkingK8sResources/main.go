package main

import (
	"client-go/config"
	"client-go/internal/app/checkingContainerImage"
	"os"
)

func main() {
	kubeConfig := os.Getenv("KUBE_CONFIG")
	if kubeConfig == "" {
		kubeConfig = "local"
	}

	clientSet, err := config.GetKubeClientSet(kubeConfig)
	if err != nil {
		panic(err.Error())
	}

	checkingContainerImage.CheckingContainerImage(clientSet)
}

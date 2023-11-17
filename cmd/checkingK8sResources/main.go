package main

import (
	"client-go/config"
	"client-go/internal/app/checkingContainerImage"
)

func main() {
	clientSet, err := config.GetKubeClientSet("local")
	if err != nil {
		panic(err.Error())
	}

	checkingContainerImage.CheckingContainerImage(clientSet)
}

package pod_metadata

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func listDeployments(clientset *kubernetes.Clientset) ([]appsv1.Deployment, []string, error) {
	namespaces, err := clientset.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("could not list namespaces: %w", err)
	}

	var deployments []appsv1.Deployment
	var nsNames []string
	for _, ns := range namespaces.Items {
		if strings.HasPrefix(ns.Name, "prd-") || strings.HasPrefix(ns.Name, "product-") {
			deps, err := clientset.AppsV1().Deployments(ns.Name).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				fmt.Printf("could not list deployments in namespace %s: %v\n", ns.Name, err)
				continue
			}
			for _, dep := range deps.Items {
				deployments = append(deployments, dep)
				nsNames = append(nsNames, ns.Name)
			}
		}
	}
	return deployments, nsNames, nil
}

func extractResources(dep appsv1.Deployment) (cpuRequest, memLimit string) {
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) > 0 {
		// Excluding sidecars like filebeat and nginx
		for _, container := range containers {
			if container.Name == "filebeat" || container.Name == "nginx" {
				continue
			}
			resources := container.Resources
			if req, ok := resources.Requests[corev1.ResourceCPU]; ok {
				cpuRequest = req.String()
			}
			if lim, ok := resources.Limits[corev1.ResourceMemory]; ok {
				memLimit = lim.String()
			}
			// Only the first main container is considered
			break
		}
	}
	return
}

func GetPodMetadata(clientset *kubernetes.Clientset) error {
	deployments, nsNames, err := listDeployments(clientset)
	if err != nil {
		fmt.Printf("Error listing deployments: %v\n", err)
		return err
	}

	file, err := os.Create("deployment_resources.csv")
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write([]string{"Namespace", "Deployment Name", "CPU Request", "Memory Limit"}); err != nil {
		fmt.Printf("Error writing header: %v\n", err)
		return err
	}

	for i, dep := range deployments {
		cpuRequest, memLimit := extractResources(dep)
		if cpuRequest != "" && memLimit != "" {
			if err := writer.Write([]string{nsNames[i], dep.Name, cpuRequest, memLimit}); err != nil {
				fmt.Printf("Error writing to CSV: %v\n", err)
				continue
			}
		}
	}
	fmt.Println("CSV generation complete!")

	return nil
}

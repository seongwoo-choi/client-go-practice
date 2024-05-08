package evictedpod

import (
	"context"
	"fmt"
	"os"
	"sync"

	log "github.com/sirupsen/logrus"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EvictedPods finds and deletes all evicted pods except those in protected namespaces.
func EvictedPods(clientSet *kubernetes.Clientset) ([]string, error) {
	evictedPods, err := listEvictedPods(clientSet)
	if err != nil {
		log.WithError(err).Error("Failed to list evicted pods")
		return nil, err
	}

	var wg sync.WaitGroup
	deletedPods := make([]string, 0, len(evictedPods))

	for _, pod := range evictedPods {
		wg.Add(1)
		deletedPods = append(deletedPods, pod.Name)
		go deletePod(clientSet, pod, &wg)
	}
	wg.Wait()

	return deletedPods, nil
}

// listEvictedPods lists all evicted pods that are not in protected namespaces.
func listEvictedPods(clientSet *kubernetes.Clientset) ([]coreV1.Pod, error) {
	evictedPods, err := clientSet.CoreV1().Pods("").List(context.TODO(), v1.ListOptions{
		FieldSelector: "status.phase=Failed",
	})
	if err != nil {
		return nil, err
	}

	var filteredPods []coreV1.Pod
	for _, pod := range evictedPods.Items {
		if pod.Status.Reason == "Evicted" && isPodDeletable(pod) {
			log.Info("Found evicted pod: ", pod.Name)
			filteredPods = append(filteredPods, pod)
		}
	}

	return filteredPods, nil
}

// deletePod attempts to delete a given pod and logs any errors.
func deletePod(clientSet *kubernetes.Clientset, pod coreV1.Pod, wg *sync.WaitGroup) {
	defer wg.Done()
	err := clientSet.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, v1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		log.WithError(err).Error(fmt.Sprintf("Error deleting pod %s", pod.Name))
	}
}

// isPodDeletable checks if the pod is not in a protected namespace.
func isPodDeletable(pod coreV1.Pod) bool {
	// Retrieve protected namespaces from environment variables.
	protectedNamespaces := []string{
		"kube-system",
		os.Getenv("DO_NOT_EVICTED_POD_NAMESPACE"),
		os.Getenv("DO_NOT_EVICTED_POD_NAMESPACE_DEFAULT"),
	}

	for _, ns := range protectedNamespaces {
		if pod.Namespace == ns {
			return false
		}
	}
	return true
}

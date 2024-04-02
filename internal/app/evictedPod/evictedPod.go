package evictedpod

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3/log"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func EvictedPods(clientSet *kubernetes.Clientset) error {
	var wg sync.WaitGroup

	evictedPods, err := clientSet.CoreV1().Pods("").List(context.TODO(), v1.ListOptions{
		FieldSelector: "status.phase=Failed",
	})

	if err != nil {
		return err
	}

	for _, evictedPod := range evictedPods.Items {
		if evictedPod.Status.Reason == "Evicted" && evictedPod.GetNamespace() != "kube-system" {
			wg.Add(1)
			log.Info(fmt.Sprintf("Evicted Pod: %s", evictedPod.Name))
			go deletePod(clientSet, evictedPod, &wg)
		}
	}
	wg.Wait()
	return nil
}

func deletePod(clientSet *kubernetes.Clientset, pod coreV1.Pod, wg *sync.WaitGroup) {
	err := clientSet.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, v1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		fmt.Printf("Error deleting pod %s: %s\n", pod.Name, err.Error())
	}
	time.Sleep(time.Millisecond * 50)
	wg.Done()
}

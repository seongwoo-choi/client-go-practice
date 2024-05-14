package main

import (
	"client-go/config"
	"client-go/internal/app/checking_deployment"
	evictedpod "client-go/internal/app/evicted_pod"
	nodeDiskUsage "client-go/internal/app/node"
	"client-go/internal/app/pod_metadata"

	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/monitor"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

func main() {
	app := fiber.New()

	kubeConfig := os.Getenv("KUBE_CONFIG")
	if kubeConfig == "" {
		kubeConfig = "local"
	}

	clientSet, err := config.GetKubeClientSet(kubeConfig)
	if err != nil {
		panic(err.Error())
	}

	app.Use(logger.New(logger.Config{
		Format:     "[${ip}] ${status} - ${method} ${path}\n",
		TimeFormat: "02-Jan-2006 15:04:05",
		TimeZone:   "Asia/Seoul",
	}))

	app.Use(recover.New())

	app.Use(healthcheck.New(healthcheck.Config{
		ReadinessEndpoint: "/monitor/healthcheck",
	}))

	file, err := os.OpenFile("./.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer file.Close()
	app.Use(logger.New(logger.Config{
		Output: file,
	}))

	err = godotenv.Load("../.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	app.Get("/metrics", monitor.New())

	apiV1 := app.Group("/api/v1")

	apiV1.Get("/evicted-pods", func(c *fiber.Ctx) error {
		deletedPods, err := evictedpod.EvictedPods(clientSet)
		if err != nil {
			log.Error(err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"msg": err.Error(),
			})
		}
		return c.Status(fiber.StatusOK).JSON(deletedPods)
	})

	apiV1.Get("/node-disk-usage", func(c *fiber.Ctx) error {
		percentage := c.Query("percentage")
		if percentage == "" {
			percentage = "70"
		}
		result, err := nodeDiskUsage.NodeDiskUsage(clientSet, percentage)
		if err != nil {
			log.Error(err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"msg": err.Error(),
			})
		}
		log.Info(result)
		return c.Status(fiber.StatusOK).JSON(result)
	})

	apiV1.Get("/node-drain", func(c *fiber.Ctx) error {
		percentage := c.Query("percentage")
		if percentage == "" {
			percentage = "70"
		}

		// 고루틴을 사용하여 NodeDrain 함수를 비동기적으로 실행
		go func() {
			err := nodeDiskUsage.NodeDrain(clientSet, percentage)
			if err != nil {
				// 비동기 처리 중 오류가 발생한 경우, 여기서는 로그만 기록하고 있습니다.
				log.Error(err)
				// 클라이언트에게 즉시 응답이 반환되므로, 오류 처리를 위한 콜백이나 상태 조회 API가 필요...할수도 있습니다..
			}
		}()

		// 오래 걸리는 작업을 비동기적으로 처리하는 동안, 즉시 응답을 반환
		return c.Status(fiber.StatusAccepted).SendString("Node drain process started")
	})

	apiV1.Get("/checking-container-image", func(c *fiber.Ctx) error {
		// 고루틴을 사용하여 NodeDrain 함수를 비동기적으로 실행
		go func() {
			checking_deployment.CheckingContainerImage(clientSet)
		}()
		return c.Status(fiber.StatusAccepted).SendString("Checking container image process started")
	})

	apiV1.Get("/pod-metadata", func(c *fiber.Ctx) error {
		err := pod_metadata.GetPodMetadata(clientSet)
		if err != nil {
			log.Error(err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"msg": err.Error(),
			})
		}
		return c.Status(fiber.StatusAccepted).SendString("pod metadata")
	})

	log.Fatal(app.Listen(":3000"))
}

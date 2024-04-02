package main

import (
	"client-go/config"
	evictedpod "client-go/internal/app/evictedPod"
	"client-go/internal/app/nodeDiskUsage"
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

	file, err := os.OpenFile("./file.log", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer file.Close()
	app.Use(logger.New(logger.Config{
		Output: file,
	}))

	err = godotenv.Load("../../.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	app.Get("/metrics", monitor.New())

	apiV1 := app.Group("/api/v1")

	apiV1.Get("/evicted-pods", func(c *fiber.Ctx) error {
		err := evictedpod.EvictedPods(clientSet)
		if err != nil {
			log.Error(err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"msg": err.Error(),
			})
		}
		return c.Status(fiber.StatusNoContent).JSON(fiber.Map{})
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

	// checkingContainerImage.CheckingContainerImage(clientSet)

	log.Fatal(app.Listen(":3000"))
}

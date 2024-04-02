package main

import (
	"client-go/config"
	drainnode "client-go/internal/app/drainNode"
	evictedpod "client-go/internal/app/evictedPod"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/gofiber/fiber/v2/middleware/healthcheck"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/monitor"
	"github.com/gofiber/fiber/v2/middleware/recover"
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

	apiV1.Get("/drain-node", func(c *fiber.Ctx) error {
		err := drainnode.DrainNode(clientSet)
		if err != nil {
			log.Error(err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"msg": err.Error(),
			})
		}
		return c.Status(fiber.StatusNoContent).JSON(fiber.Map{})
	})

	// checkingContainerImage.CheckingContainerImage(clientSet)

	log.Fatal(app.Listen(":3000"))
}

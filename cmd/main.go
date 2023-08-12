package main

import (
	"context"
	"errors"
	"github.com/sirupsen/logrus"
	"go-service-template/internal/api"
	"go-service-template/internal/infrastructure"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Retrieve the shared logger instance
	log := infrastructure.GetLog()

	// Load the configuration necessary for the server
	config, err := infrastructure.LoadDefaultConfig()

	if err != nil {
		log.WithError(err).Fatal("Unable to load configuration")
	}

	// Create a new server with the loaded configuration
	server := api.NewServer(config)

	// Start the server asynchronously
	go func() {
		log.WithFields(logrus.Fields{"address": server.Address()}).Info("Server is ready to handle requests")
		if err := server.Start(); err != nil {
			log.Errorf("Error while serving: %s", err.Error())
			os.Exit(1)
		}
	}()

	// Create a new metrics server with the specified address
	metricsServer := api.NewMetricsServer(&config.Metrics)

	if config.Metrics.IsEnabled {
		// Start the metrics server asynchronously
		go func() {
			log.WithFields(logrus.Fields{"address": metricsServer.Address()}).Info("Metrics server is ready to handle requests")
			if err := metricsServer.Start(); err != nil {
				if !errors.Is(err, http.ErrServerClosed) {
					log.Errorf("Error while serving metrics: %s", err.Error())
					os.Exit(1)
				}
			}
		}()
	}

	// Await termination signal (e.g., from operating system)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	<-stop

	// Initiate server shutdown, allowing ongoing operations a brief grace period
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.WithFields(logrus.Fields{"error": err}).Error("Server Shutdown Failed")
	} else {
		log.Info("Server exited properly")
	}

	if config.Metrics.IsEnabled {
		// Shutdown the metrics server
		if err := metricsServer.Shutdown(ctx); err != nil {
			log.WithFields(logrus.Fields{"error": err}).Error("Metrics Server Shutdown Failed")
		} else {
			log.Info("Metrics server exited properly")
		}
	}
}

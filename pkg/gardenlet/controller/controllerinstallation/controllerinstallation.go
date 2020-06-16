// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllerinstallation

import (
	"context"
	"sync"
	"time"

	gardencoreinformers "github.com/gardener/gardener/pkg/client/core/informers/externalversions"
	gardencorelisters "github.com/gardener/gardener/pkg/client/core/listers/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/gardenlet"
	"github.com/gardener/gardener/pkg/gardenlet/apis/config"
	confighelper "github.com/gardener/gardener/pkg/gardenlet/apis/config/helper"
	"github.com/gardener/gardener/pkg/logger"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

// FinalizerName is the name of the ControllerInstallation finalizer.
const FinalizerName = "core.gardener.cloud/controllerinstallation"

// Controller controls ControllerInstallation.
type Controller struct {
	config *config.GardenletConfiguration

	controllerInstallationControl ControlInterface
	careControl                   CareControlInterface

	recorder record.EventRecorder

	seedQueue  workqueue.RateLimitingInterface
	seedLister gardencorelisters.SeedLister
	seedSynced cache.InformerSynced

	controllerRegistrationSynced cache.InformerSynced

	controllerInstallationQueue  workqueue.RateLimitingInterface
	controllerInstallationLister gardencorelisters.ControllerInstallationLister
	controllerInstallationSynced cache.InformerSynced

	controllerInstallationCareQueue workqueue.RateLimitingInterface

	workerCh               chan int
	numberOfRunningWorkers int
}

// NewController instantiates a new ControllerInstallation controller.
func NewController(clientMap clientmap.ClientMap, gardenCoreInformerFactory gardencoreinformers.SharedInformerFactory, config *config.GardenletConfiguration, recorder record.EventRecorder, gardenNamespace *corev1.Namespace) *Controller {
	var (
		gardenCoreInformer = gardenCoreInformerFactory.Core().V1beta1()

		seedInformer = gardenCoreInformer.Seeds()
		seedLister   = seedInformer.Lister()
		seedQueue    = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "seed")

		controllerRegistrationInformer = gardenCoreInformer.ControllerRegistrations()
		controllerRegistrationLister   = controllerRegistrationInformer.Lister()

		controllerInstallationInformer = gardenCoreInformer.ControllerInstallations()
		controllerInstallationLister   = controllerInstallationInformer.Lister()
		controllerInstallationQueue    = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "controllerinstallation")

		controllerInstallationCareQueue = workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "controllerinstallation-care")
	)

	controller := &Controller{
		controllerInstallationControl: NewDefaultControllerInstallationControl(clientMap, gardenCoreInformerFactory, recorder, config, seedLister, controllerRegistrationLister, controllerInstallationLister, gardenNamespace),
		careControl:                   NewDefaultCareControl(clientMap, config),

		config:   config,
		recorder: recorder,

		seedLister: seedLister,
		seedQueue:  seedQueue,

		controllerInstallationLister: controllerInstallationLister,
		controllerInstallationQueue:  controllerInstallationQueue,

		controllerInstallationCareQueue: controllerInstallationCareQueue,

		workerCh: make(chan int),
	}

	controller.seedSynced = seedInformer.Informer().HasSynced
	controller.controllerRegistrationSynced = controllerRegistrationInformer.Informer().HasSynced

	controllerInstallationInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controllerutils.ControllerInstallationFilterFunc(confighelper.SeedNameFromSeedConfig(config.SeedConfig), seedLister, config.SeedSelector),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    controller.controllerInstallationAdd,
			UpdateFunc: controller.controllerInstallationUpdate,
			DeleteFunc: controller.controllerInstallationDelete,
		},
	})

	controllerInstallationInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controllerutils.ControllerInstallationFilterFunc(confighelper.SeedNameFromSeedConfig(config.SeedConfig), seedLister, config.SeedSelector),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: controller.controllerInstallationCareAdd,
		},
	})

	controller.controllerInstallationSynced = controllerInstallationInformer.Informer().HasSynced

	return controller
}

// Run runs the Controller until the given stop channel can be read from.
func (c *Controller) Run(ctx context.Context, workers, careWorkers int) {
	var waitGroup sync.WaitGroup

	if !cache.WaitForCacheSync(ctx.Done(), c.seedSynced, c.controllerRegistrationSynced, c.controllerInstallationSynced) {
		logger.Logger.Error("Timed out waiting for caches to sync")
		return
	}

	go func() {
		for res := range c.workerCh {
			c.numberOfRunningWorkers += res
			logger.Logger.Debugf("Current number of running ControllerInstallation workers is %d", c.numberOfRunningWorkers)
		}
	}()

	logger.Logger.Info("ControllerInstallation controller initialized.")

	for i := 0; i < workers; i++ {
		controllerutils.DeprecatedCreateWorker(ctx, c.controllerInstallationQueue, "ControllerInstallation", c.reconcileControllerInstallationKey, &waitGroup, c.workerCh)
	}

	for i := 0; i < careWorkers; i++ {
		controllerutils.DeprecatedCreateWorker(ctx, c.controllerInstallationCareQueue, "ControllerInstallation Care", c.reconcileControllerInstallationCareKey, &waitGroup, c.workerCh)
	}

	// Shutdown handling
	<-ctx.Done()
	c.controllerInstallationQueue.ShutDown()
	c.controllerInstallationCareQueue.ShutDown()

	for {
		if c.controllerInstallationQueue.Len() == 0 && c.controllerInstallationCareQueue.Len() == 0 && c.numberOfRunningWorkers == 0 {
			logger.Logger.Debug("No running ControllerInstallation worker and no items left in the queues. Terminated ControllerInstallation controller...")
			break
		}
		logger.Logger.Debugf("Waiting for %d ControllerInstallation worker(s) to finish (%d item(s) left in the queues)...", c.numberOfRunningWorkers, c.controllerInstallationQueue.Len()+c.controllerInstallationCareQueue.Len())
		time.Sleep(5 * time.Second)
	}

	waitGroup.Wait()
}

// RunningWorkers returns the number of running workers.
func (c *Controller) RunningWorkers() int {
	return c.numberOfRunningWorkers
}

// CollectMetrics implements gardenmetrics.ControllerMetricsCollector interface
func (c *Controller) CollectMetrics(ch chan<- prometheus.Metric) {
	metric, err := prometheus.NewConstMetric(gardenlet.ControllerWorkerSum, prometheus.GaugeValue, float64(c.RunningWorkers()), "controllerinstallation")
	if err != nil {
		gardenlet.ScrapeFailures.With(prometheus.Labels{"kind": "controllerinstallation-controller"}).Inc()
		return
	}
	ch <- metric
}

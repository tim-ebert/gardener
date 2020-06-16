// Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package backupentry

import (
	"context"
	"sync"
	"time"

	gardencoreinformers "github.com/gardener/gardener/pkg/client/core/informers/externalversions"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/gardenlet"
	"github.com/gardener/gardener/pkg/gardenlet/apis/config"
	confighelper "github.com/gardener/gardener/pkg/gardenlet/apis/config/helper"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/gardener/gardener/pkg/logger"
)

// Controller controls BackupEntries.
type Controller struct {
	config     *config.GardenletConfiguration
	reconciler reconcile.Reconciler
	recorder   record.EventRecorder

	backupEntryQueue  workqueue.RateLimitingInterface
	backupEntrySynced cache.InformerSynced

	workerCh               chan int
	numberOfRunningWorkers int
}

// NewBackupEntryController takes a Kubernetes client for the Garden clusters <k8sGardenClient>, a struct
// holding information about the acting Gardener, a <backupEntryInformer>, and a <recorder> for
// event recording. It creates a new Gardener controller.
func NewBackupEntryController(clientMap clientmap.ClientMap, gardenCoreInformerFactory gardencoreinformers.SharedInformerFactory, config *config.GardenletConfiguration, recorder record.EventRecorder) *Controller {
	var (
		gardencorev1beta1Informer = gardenCoreInformerFactory.Core().V1beta1()
		backupEntryInformer       = gardencorev1beta1Informer.BackupEntries()

		seedInformer = gardencorev1beta1Informer.Seeds()
		seedLister   = seedInformer.Lister()
	)

	backupEntryController := &Controller{
		config:           config,
		reconciler:       newReconciler(context.TODO(), clientMap, recorder, config),
		recorder:         recorder,
		backupEntryQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "BackupEntry"),
		workerCh:         make(chan int),
	}

	backupEntryInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: controllerutils.BackupEntryFilterFunc(confighelper.SeedNameFromSeedConfig(config.SeedConfig), seedLister, config.SeedSelector),
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    backupEntryController.backupEntryAdd,
			UpdateFunc: backupEntryController.backupEntryUpdate,
			DeleteFunc: backupEntryController.backupEntryDelete,
		},
	})

	backupEntryController.backupEntrySynced = backupEntryInformer.Informer().HasSynced

	return backupEntryController
}

// Run runs the Controller until the given stop channel can be read from.
func (c *Controller) Run(ctx context.Context, workers int) {
	var waitGroup sync.WaitGroup

	if !cache.WaitForCacheSync(ctx.Done(), c.backupEntrySynced) {
		logger.Logger.Error("Timed out waiting for caches to sync")
		return
	}

	// Count number of running workers.
	go func() {
		for res := range c.workerCh {
			c.numberOfRunningWorkers += res
			logger.Logger.Debugf("Current number of running BackupEntry workers is %d", c.numberOfRunningWorkers)
		}
	}()

	logger.Logger.Info("BackupEntry controller initialized.")

	for i := 0; i < workers; i++ {
		controllerutils.CreateWorker(ctx, c.backupEntryQueue, "backupentry", c.reconciler, &waitGroup, c.workerCh)
	}

	// Shutdown handling
	<-ctx.Done()
	c.backupEntryQueue.ShutDown()

	for {
		if c.backupEntryQueue.Len() == 0 && c.numberOfRunningWorkers == 0 {
			logger.Logger.Info("No running BackupEntry worker and no items left in the queues. Terminated BackupEntry controller...")
			break
		}
		logger.Logger.Infof("Waiting for %d BackupEntry worker(s) to finish (%d item(s) left in the queues)...", c.numberOfRunningWorkers, c.backupEntryQueue.Len())
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
	metric, err := prometheus.NewConstMetric(gardenlet.ControllerWorkerSum, prometheus.GaugeValue, float64(c.RunningWorkers()), "backupentry")
	if err != nil {
		gardenlet.ScrapeFailures.With(prometheus.Labels{"kind": "backupentry-controller"}).Inc()
		return
	}
	ch <- metric
}

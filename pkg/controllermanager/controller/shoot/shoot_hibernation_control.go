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

package shoot

import (
	"reflect"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	gardenlogger "github.com/gardener/gardener/pkg/logger"

	"github.com/robfig/cron"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
)

func hibernationLogger(key string) logrus.FieldLogger {
	return gardenlogger.NewFieldLogger(gardenlogger.Logger, "shoot-hibernation", key)
}

func getShootHibernationSchedules(shoot *gardencorev1beta1.Shoot) []gardencorev1beta1.HibernationSchedule {
	hibernation := shoot.Spec.Hibernation
	if hibernation == nil {
		return nil
	}
	return hibernation.Schedules
}

var (
	// NewCronWithLocation creates a new cron with the given location. Exposed for testing.
	NewCronWithLocation = newCronWithLocation

	// TimeNow returns the current time. Exposed for testing.
	TimeNow = time.Now
)

func newCronWithLocation(location *time.Location) Cron {
	return cron.NewWithLocation(location)
}

// GroupHibernationSchedulesByLocation groups the given HibernationSchedules by their Location.
// If the Location of a HibernationSchedule is `nil`, it is defaulted to UTC.
func GroupHibernationSchedulesByLocation(schedules []gardencorev1beta1.HibernationSchedule) map[string][]gardencorev1beta1.HibernationSchedule {
	var (
		locationToSchedules = make(map[string][]gardencorev1beta1.HibernationSchedule)
	)

	for _, schedule := range schedules {
		var locationID string
		if schedule.Location != nil {
			locationID = *schedule.Location
		} else {
			locationID = time.UTC.String()
		}

		locationToSchedules[locationID] = append(locationToSchedules[locationID], schedule)
	}

	return locationToSchedules
}

// LocationLogger returns a logger for the given location.
func LocationLogger(logger logrus.FieldLogger, location *time.Location) logrus.FieldLogger {
	return logger.WithFields(logrus.Fields{
		"location": location,
	})
}

// ComputeHibernationSchedule computes the HibernationSchedule for the given Shoot.
func ComputeHibernationSchedule(clientMap clientmap.ClientMap, logger logrus.FieldLogger, shoot *gardencorev1beta1.Shoot) (HibernationSchedule, error) {
	var (
		schedules           = getShootHibernationSchedules(shoot)
		locationToSchedules = GroupHibernationSchedulesByLocation(schedules)
		schedule            = make(HibernationSchedule, len(locationToSchedules))
	)

	for locationID, schedules := range locationToSchedules {
		location, err := time.LoadLocation(locationID)
		if err != nil {
			return nil, err
		}

		cr := NewCronWithLocation(location)
		cronLogger := LocationLogger(logger, location)
		for _, schedule := range schedules {
			if schedule.Start != nil {
				start, err := cron.ParseStandard(*schedule.Start)
				if err != nil {
					return nil, err
				}

				cr.Schedule(start, NewHibernationJob(clientMap, cronLogger, shoot, true))
				cronLogger.Debugf("Next hibernation for spec %q will trigger at %v", *schedule.Start, start.Next(TimeNow().UTC()))
			}

			if schedule.End != nil {
				end, err := cron.ParseStandard(*schedule.End)
				if err != nil {
					return nil, err
				}

				cr.Schedule(end, NewHibernationJob(clientMap, cronLogger, shoot, false))
				cronLogger.Debugf("Next wakeup for spec %q will trigger at %v", *schedule.End, end.Next(TimeNow().UTC()))
			}
		}
		schedule[locationID] = cr
	}

	return schedule, nil
}

func shootHasHibernationSchedules(shoot *gardencorev1beta1.Shoot) bool {
	return getShootHibernationSchedules(shoot) != nil
}

func (c *Controller) shootHibernationAdd(obj interface{}) {
	shoot, ok := obj.(*gardencorev1beta1.Shoot)
	if !ok {
		return
	}

	if shootHasHibernationSchedules(shoot) {
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			gardenlogger.Logger.Errorf("Couldn't get key for object %+v: %v", obj, err)
			return
		}
		c.shootHibernationQueue.Add(key)
	}
}

func (c *Controller) shootHibernationUpdate(oldObj, newObj interface{}) {
	var (
		oldShoot = oldObj.(*gardencorev1beta1.Shoot)
		newShoot = newObj.(*gardencorev1beta1.Shoot)

		oldSchedule = getShootHibernationSchedules(oldShoot)
		newSchedule = getShootHibernationSchedules(newShoot)
	)

	if !reflect.DeepEqual(oldSchedule, newSchedule) {
		key, err := cache.MetaNamespaceKeyFunc(newObj)
		if err != nil {
			gardenlogger.Logger.Errorf("Couldn't get key for object %+v: %v", newObj, err)
			return
		}
		c.shootHibernationQueue.Add(key)
	}
}

func (c *Controller) shootHibernationDelete(obj interface{}) {
	shoot, ok := obj.(*gardencorev1beta1.Shoot)
	if !ok {
		return
	}

	if shootHasHibernationSchedules(shoot) {
		key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
		if err != nil {
			gardenlogger.Logger.Errorf("Couldn't get key for object %+v: %v", obj, err)
			return
		}
		c.shootHibernationQueue.Add(key)
	}
}

func (c *Controller) deleteShootCron(logger logrus.FieldLogger, key string) {
	if sched, ok := c.hibernationScheduleRegistry.Load(key); ok {
		sched.Stop()
		logger.Debugf("Stopped cron")
	}
	c.hibernationScheduleRegistry.Delete(key)
	logger.Debugf("Deleted cron")
}

func (c *Controller) reconcileShootHibernationKey(key string) error {
	logger := hibernationLogger(key)
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	shoot, err := c.shootLister.Shoots(namespace).Get(name)
	if apierrors.IsNotFound(err) {
		c.deleteShootCron(logger, key)
		logger.Debugf("Skipping because Shoot has been deleted", key)
		return nil
	}
	if err != nil {
		logger.Debugf("Unable to retrieve object from store: %v", key, err)
		return err
	}

	logger.Info("[SHOOT HIBERNATION]")

	if shoot.DeletionTimestamp != nil {
		c.deleteShootCron(logger, key)
		return nil
	}
	return c.reconcileShootHibernation(logger, key, shoot.DeepCopy())
}

func (c *Controller) reconcileShootHibernation(logger logrus.FieldLogger, key string, shoot *gardencorev1beta1.Shoot) error {
	c.deleteShootCron(logger, key)
	if !shootHasHibernationSchedules(shoot) {
		return nil
	}

	schedule, err := ComputeHibernationSchedule(c.clientMap, logger, shoot)
	if err != nil {
		return err
	}

	schedule.Start()

	c.hibernationScheduleRegistry.Store(key, schedule)
	logger.Debugf("Successfully started hibernation schedule")

	return nil
}

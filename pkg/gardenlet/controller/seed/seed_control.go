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

package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/logger"
	"github.com/gardener/gardener/pkg/operation/common"
	seedpkg "github.com/gardener/gardener/pkg/operation/seed"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func (c *Controller) seedAdd(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		logger.Logger.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	c.seedQueue.Add(key)
}

func (c *Controller) seedUpdate(oldObj, newObj interface{}) {
	var (
		oldSeed = oldObj.(*gardencorev1beta1.Seed)
		newSeed = newObj.(*gardencorev1beta1.Seed)
	)

	if oldSeed.Generation == newSeed.Generation {
		return
	}
	c.seedAdd(newObj)
}

func (c *Controller) seedDelete(obj interface{}) {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		logger.Logger.Errorf("Couldn't get key for object %+v: %v", obj, err)
		return
	}
	c.seedQueue.Add(key)
}

func (c *Controller) reconcileSeedRequest(req reconcile.Request) (reconcile.Result, error) {
	name := req.Name
	seed, err := c.seedLister.Get(name)
	if apierrors.IsNotFound(err) {
		logger.Logger.Debugf("[SEED RECONCILE] %s - skipping because Seed has been deleted", name)
		return reconcile.Result{}, nil
	}
	if err != nil {
		logger.Logger.Infof("[SEED RECONCILE] %s - unable to retrieve object from store: %v", name, err)
		return reconcile.Result{}, err
	}

	return c.ReconcileSeed(seed, name)
}

func (c *Controller) ReconcileSeed(obj *gardencorev1beta1.Seed, key string) (reconcile.Result, error) {
	// TODO (tim-ebert): remove
	if obj.Name != "staging-aws" {
		return reconcile.Result{}, nil
	}

	var (
		ctx         = context.TODO()
		seed        = obj.DeepCopy()
		seedJSON, _ = json.Marshal(seed)
		seedLogger  = logger.NewFieldLogger(logger.Logger, "seed", seed.Name)
		err         error

		conditionSeedBootstrapped = gardencorev1beta1helper.GetOrInitCondition(seed.Status.Conditions, gardencorev1beta1.SeedBootstrapped)
	)

	// The deletionTimestamp labels a Seed as intended to get deleted. Before deletion,
	// it has to be ensured that no Shoots are depending on the Seed anymore.
	// When this happens the controller will remove the finalizers from the Seed so that it can be garbage collected.
	if seed.DeletionTimestamp != nil {
		if !controllerutils.HasFinalizer(seed, gardencorev1beta1.GardenerName) {
			// don't requeue Seed if it has been deleted
			return reconcile.Result{}, nil
		}

		if seed.Spec.Backup != nil {
			if err := deleteBackupBucketInGarden(ctx, c.k8sGardenClient.Client(), seed); err != nil {
				seedLogger.Error(err.Error())
				return reconcile.Result{}, err
			}
		}

		associatedShoots, err := controllerutils.DetermineShootsAssociatedTo(seed, c.shootLister)
		if err != nil {
			seedLogger.Error(err.Error())
			return reconcile.Result{}, err
		}
		// As per design, backupBucket's are not tightly coupled with Seed resources. But to reconcile backup bucket on object store, seed
		// provides the worker node for running backup extension controller. Hence, we do check if there is another Seed available for
		// running this backup extension controller for associated backup buckets. Otherwise we block the deletion of current seed.
		// validSeedBootstrapped, err := validSeedBootstrappedForBucketRescheduling(ctx, c.k8sGardenClient.Client())
		// if err != nil {
		// 	seedLogger.Error(err.Error())
		// 	return reconcile.Result{}, err
		// }
		// associatedBackupBuckets := make([]string, 0)

		//if validSeedBootstrapped {
		associatedBackupBuckets, err := controllerutils.DetermineBackupBucketAssociations(ctx, c.k8sGardenClient.Client(), seed.Name)
		if err != nil {
			seedLogger.Error(err.Error())
			return reconcile.Result{}, err
		}
		//}
		if len(associatedShoots) == 0 && len(associatedBackupBuckets) == 0 {
			seedLogger.Info("No Shoots or BackupBuckets are referencing the Seed. Deletion accepted.")

			seedObj := seedpkg.New(seed)
			if err := seedObj.InitializeClients(ctx, c.k8sGardenClient.Client(), c.config.SeedClientConnection.ClientConnectionConfiguration, c.config.SeedSelector == nil); err != nil {
				msg := fmt.Sprintf("Failed to initialize Seed clients for Seed deletion: %+v", err)
				seedLogger.Error(msg)
				conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionUnknown, gardencorev1beta1.ConditionCheckError, msg)
				c.updateSeedStatus(seed, "", conditionSeedBootstrapped)
				return reconcile.Result{}, err
			}

			conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionProgressing, "CleanupProgressing", "Seed cluster is currently being cleaned up")
			c.updateSeedStatus(seed, "", conditionSeedBootstrapped)

			if err := seedpkg.CleanupSeed(ctx, seedObj, seedLogger, func(msg string) {
				conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionProgressing, "CleanupProgressing", "Seed cluster is currently being cleaned up: "+msg)
				c.updateSeedStatus(seed, "", conditionSeedBootstrapped)
			}); err != nil {
				msg := fmt.Sprintf("Failed to cleanup Seed: %+v", err)
				seedLogger.Error(msg)
				conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionFalse, "CleanupFailed", msg)
				c.updateSeedStatus(seed, "", conditionSeedBootstrapped)
				return reconcile.Result{}, err
			}

			seedLogger.Infof("Cleanup of Seed has been successful")
			conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionFalse, "CleanupSucceeded", "Cleanup of Seed has been successful")
			c.updateSeedStatus(seed, "", conditionSeedBootstrapped)

			if seed.Spec.SecretRef != nil {
				// Remove finalizer from referenced secret
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      seed.Spec.SecretRef.Name,
						Namespace: seed.Spec.SecretRef.Namespace,
					},
				}
				if err := controllerutils.RemoveFinalizer(ctx, c.k8sGardenClient.Client(), secret, gardencorev1beta1.ExternalGardenerName); err != nil {
					seedLogger.Error(err.Error())
					return reconcile.Result{}, err
				}
			}

			// Remove finalizer from Seed
			if err := controllerutils.RemoveGardenerFinalizer(ctx, c.k8sGardenClient.Client(), seed); err != nil {
				seedLogger.Error(err.Error())
				return reconcile.Result{}, err
			}

			// don't requeue Seed if it has been deleted
			return reconcile.Result{}, nil
		}

		parentLogMessage := "Can't delete Seed, because the following objects are still referencing it:"
		if len(associatedShoots) != 0 {
			message := fmt.Sprintf("%s Shoots=%v", parentLogMessage, associatedShoots)
			seedLogger.Info(message)
			c.recorder.Event(seed, corev1.EventTypeNormal, v1beta1constants.EventResourceReferenced, message)
		}
		if len(associatedBackupBuckets) != 0 {
			message := fmt.Sprintf("%s BackupBuckets=%v", parentLogMessage, associatedBackupBuckets)
			seedLogger.Info(message)
			c.recorder.Event(seed, corev1.EventTypeNormal, v1beta1constants.EventResourceReferenced, message)
		}

		// wait for explicitly specified duration, to not run into exponential backoff too fast while waiting for references
		return reconcile.Result{RequeueAfter: 15 * time.Second}, errors.New("seed still has references")
	}

	seedLogger.Infof("[SEED RECONCILE] %s", key)
	seedLogger.Debugf(string(seedJSON))

	err = controllerutils.EnsureFinalizer(ctx, c.k8sGardenClient.Client(), seed, gardencorev1beta1.GardenerName)
	if err != nil {
		err = fmt.Errorf("could not add finalizer to Seed: %s", err.Error())
		seedLogger.Error(err)
		return reconcile.Result{}, err
	}

	// Add the Gardener finalizer to the referenced Seed secret to protect it from deletion as long as the Seed resource
	// does exist.
	if seed.Spec.SecretRef != nil {
		secret, err := common.GetSecretFromSecretRef(ctx, c.k8sGardenClient.Client(), seed.Spec.SecretRef)
		if err != nil {
			seedLogger.Error(err.Error())
			return reconcile.Result{}, err
		}
		if err := controllerutils.EnsureFinalizer(ctx, c.k8sGardenClient.Client(), secret, gardencorev1beta1.ExternalGardenerName); err != nil {
			seedLogger.Error(err.Error())
			return reconcile.Result{}, err
		}
	}

	seedObj := seedpkg.New(seed)
	if err := seedObj.InitializeClients(ctx, c.k8sGardenClient.Client(), c.config.SeedClientConnection.ClientConnectionConfiguration, c.config.SeedSelector == nil); err != nil {
		msg := fmt.Sprintf("Failed to initialize Seed clients: %+v", err)
		conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionUnknown, gardencorev1beta1.ConditionCheckError, msg)
		seedLogger.Error(msg)
		c.updateSeedStatus(seed, "<unknown>", conditionSeedBootstrapped)
		return reconcile.Result{}, err
	}

	// Check whether the Kubernetes version of the Seed cluster fulfills the minimal requirements.
	if seedKubernetesVersion, err := seedObj.CheckMinimumK8SVersion(); err != nil {
		conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionFalse, "K8SVersionTooOld", err.Error())
		c.updateSeedStatus(seed, seedKubernetesVersion, conditionSeedBootstrapped)
		seedLogger.Error(err.Error())
		return reconcile.Result{}, err
	}

	// Bootstrap the Seed cluster.
	if c.config.Controllers.Seed.ReserveExcessCapacity != nil {
		seedObj.MustReserveExcessCapacity(*c.config.Controllers.Seed.ReserveExcessCapacity)
	}
	if gardencorev1beta1helper.TaintsHave(seedObj.Info.Spec.Taints, gardencorev1beta1.SeedTaintDisableCapacityReservation) {
		seedObj.MustReserveExcessCapacity(false)
	}

	conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionProgressing, "BootstrappingPending", "Seed cluster is currently bootstrapping")
	c.updateSeedStatus(seed, "", conditionSeedBootstrapped)

	if err := seedpkg.BootstrapCluster(ctx, seedObj, seedLogger, c.secrets, c.imageVector, c.componentImageVectors); err != nil {
		conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionFalse, "BootstrappingFailed", err.Error())
		c.updateSeedStatus(seed, "", conditionSeedBootstrapped)
		seedLogger.Errorf("Seed bootstrapping failed: %+v", err)
		return reconcile.Result{}, err
	}

	// TODO (tim-ebert): introduce MR health checks in a separate controller? If we wait here, we block the Seed from being deleted / reconciled again
	if err := seedpkg.WaitUntilSeedBootstrapped(ctx, seedObj, seedLogger, func(msg string) {
		conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionProgressing, "BootstrappingPending", "Seed cluster is currently bootstrapping: "+msg)
		c.updateSeedStatus(seed, "", conditionSeedBootstrapped)
	}); err != nil {
		conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionFalse, "BootstrappingFailed", err.Error())
		c.updateSeedStatus(seed, "", conditionSeedBootstrapped)
		seedLogger.Errorf("Seed bootstrapping failed: %+v", err)
	}

	conditionSeedBootstrapped = gardencorev1beta1helper.UpdatedCondition(conditionSeedBootstrapped, gardencorev1beta1.ConditionTrue, "BootstrappingSucceeded", "Seed cluster has been bootstrapped successfully.")
	c.updateSeedStatus(seed, "", conditionSeedBootstrapped)

	if seed.Spec.Backup != nil {
		// This should be post updating the seed is available. Since, scheduler will then mostly use
		// same seed for deploying the backupBucket extension.
		if err := deployBackupBucketInGarden(ctx, c.k8sGardenClient.Client(), seed); err != nil {
			seedLogger.Error(err.Error())
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{RequeueAfter: c.config.Controllers.Seed.SyncPeriod.Duration}, nil
}

func (c *Controller) updateSeedStatus(seed *gardencorev1beta1.Seed, k8sVersion string, updateConditions ...gardencorev1beta1.Condition) {
	if _, err := kutil.TryUpdateSeedStatus(c.k8sGardenClient.GardenCore(), retry.DefaultBackoff, seed.ObjectMeta,
		func(seed *gardencorev1beta1.Seed) (*gardencorev1beta1.Seed, error) {
			// remove "available condition"
			for i, c := range seed.Status.Conditions {
				if c.Type == "Available" {
					seed.Status.Conditions = append(seed.Status.Conditions[:i], seed.Status.Conditions[i+1:]...)
					break
				}
			}

			seed.Status.Conditions = gardencorev1beta1helper.MergeConditions(seed.Status.Conditions, updateConditions...)
			seed.Status.ObservedGeneration = seed.Generation
			seed.Status.Gardener = c.identity

			if k8sVersion != "" {
				seed.Status.KubernetesVersion = &k8sVersion
			}
			return seed, nil
		},
	); err != nil {
		logger.Logger.Errorf("Could not update the Seed status: %+v", err)
	}
}

func deployBackupBucketInGarden(ctx context.Context, k8sGardenClient client.Client, seed *gardencorev1beta1.Seed) error {
	// By default, we assume the seed.Spec.Backup.Provider matches the seed.Spec.Provider.Type as per the validation logic.
	// However, if the backup region is specified we take it.
	region := seed.Spec.Provider.Region
	if seed.Spec.Backup.Region != nil {
		region = *seed.Spec.Backup.Region
	}

	backupBucket := &gardencorev1beta1.BackupBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: string(seed.UID),
		},
	}

	ownerRef := metav1.NewControllerRef(seed, gardencorev1beta1.SchemeGroupVersion.WithKind("Seed"))

	_, err := controllerutil.CreateOrUpdate(ctx, k8sGardenClient, backupBucket, func() error {
		backupBucket.OwnerReferences = []metav1.OwnerReference{*ownerRef}
		backupBucket.Spec = gardencorev1beta1.BackupBucketSpec{
			Provider: gardencorev1beta1.BackupBucketProvider{
				Type:   seed.Spec.Backup.Provider,
				Region: region,
			},
			ProviderConfig: seed.Spec.Backup.ProviderConfig,
			SecretRef: corev1.SecretReference{
				Name:      seed.Spec.Backup.SecretRef.Name,
				Namespace: seed.Spec.Backup.SecretRef.Namespace,
			},
			SeedName: &seed.Name, // In future this will be moved to gardener-scheduler.
		}
		return nil
	})
	return err
}

func deleteBackupBucketInGarden(ctx context.Context, k8sGardenClient client.Client, seed *gardencorev1beta1.Seed) error {
	backupBucket := &gardencorev1beta1.BackupBucket{
		ObjectMeta: metav1.ObjectMeta{
			Name: string(seed.UID),
		},
	}

	return client.IgnoreNotFound(k8sGardenClient.Delete(ctx, backupBucket))
}

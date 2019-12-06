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
	"context"
	"time"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	v1alpha1constants "github.com/gardener/gardener/pkg/apis/core/v1alpha1/constants"
	gardencorev1alpha1helper "github.com/gardener/gardener/pkg/apis/core/v1alpha1/helper"
	controllerutils "github.com/gardener/gardener/pkg/controllermanager/controller/utils"
	"github.com/gardener/gardener/pkg/operation"
	botanistpkg "github.com/gardener/gardener/pkg/operation/botanist"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/errors"
	"github.com/gardener/gardener/pkg/utils/flow"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	utilretry "github.com/gardener/gardener/pkg/utils/retry"
	"github.com/gardener/gardener/pkg/version"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

// runReconcileShootFlow reconciles the Shoot cluster's state.
// It receives an Operation object <o> which stores the Shoot object.
func (c *Controller) runReconcileShootFlow(o *operation.Operation, operationType gardencorev1alpha1.LastOperationType) *gardencorev1alpha1helper.WrappedLastErrors {
	// We create the botanists (which will do the actual work).
	var (
		botanist             *botanistpkg.Botanist
		enableEtcdEncryption bool
		tasksWithErrors      []string
		err                  error
	)

	for _, lastError := range o.Shoot.Info.Status.LastErrors {
		if lastError.TaskID != nil {
			tasksWithErrors = append(tasksWithErrors, *lastError.TaskID)
		}
	}

	errorContext := errors.NewErrorContext("Shoot cluster reconciliation", tasksWithErrors)

	err = errors.HandleErrors(errorContext,
		func(errorID string) error {
			o.CleanShootTaskError(context.TODO(), errorID)
			return nil
		},
		nil,
		errors.ToExecute("Create botanist", func() error {
			return utilretry.UntilTimeout(context.TODO(), 10*time.Second, 10*time.Minute, func(context.Context) (done bool, err error) {
				botanist, err = botanistpkg.New(o)
				if err != nil {
					return utilretry.MinorError(err)
				}
				return utilretry.Ok()
			})
		}),
		errors.ToExecute("Check required extensions", func() error {
			return botanist.RequiredExtensionsExist()
		}),
		errors.ToExecute("Check version constraint", func() error {
			enableEtcdEncryption, err = utils.CheckVersionMeetsConstraint(botanist.Shoot.Info.Spec.Kubernetes.Version, ">= 1.13")
			return err
		}),
	)

	if err != nil {
		return gardencorev1alpha1helper.NewWrappedLastErrors(gardencorev1alpha1helper.FormatLastErrDescription(err), err)
	}

	g := flow.NewGraph("Shoot cluster reconciliation")
	AddReconcileShootFlowTasks(g, o, botanist, enableEtcdEncryption)
	f := g.Compile()

	err = f.Run(flow.Opts{Logger: o.Logger, ProgressReporter: o.ReportShootProgress, ErrorContext: errorContext, ErrorCleaner: o.CleanShootTaskError})
	if err != nil {
		o.Logger.Errorf("Failed to reconcile Shoot %q: %+v", o.Shoot.Info.Name, err)
		return gardencorev1alpha1helper.NewWrappedLastErrors(gardencorev1alpha1helper.FormatLastErrDescription(err), flow.Errors(err))
	}

	// Register the Shoot as Seed cluster if it was annotated properly and in the garden namespace
	if o.Shoot.Info.Namespace == v1alpha1constants.GardenNamespace {
		if o.ShootedSeed != nil {
			if err := botanist.RegisterAsSeed(o.ShootedSeed.Protected, o.ShootedSeed.Visible, o.ShootedSeed.MinimumVolumeSize, o.ShootedSeed.BlockCIDRs, o.ShootedSeed.ShootDefaults, o.ShootedSeed.Backup); err != nil {
				o.Logger.Errorf("Could not register Shoot %q as Seed: %+v", o.Shoot.Info.Name, err)
			}
		} else {
			if err := botanist.UnregisterAsSeed(); err != nil {
				o.Logger.Errorf("Could not unregister Shoot %q as Seed: %+v", o.Shoot.Info.Name, err)
			}
		}
	}

	o.Logger.Infof("Successfully reconciled Shoot %q", o.Shoot.Info.Name)
	return nil
}

func AddReconcileShootFlowTasks(g flow.GraphInterface, o operation.OperationInformation, botanist *botanistpkg.Botanist, enableEtcdEncryption bool) {
	var (
		defaultTimeout     = 30 * time.Second
		defaultInterval    = 5 * time.Second
		hibernationEnabled = o.IsShootHibernationEnabled()
		managedExternalDNS = o.IsShootExternalDomainManaged()
		managedInternalDNS = o.IsGardenInternalDomainManaged()
		allowBackup        = o.IsSeedBackupEnabled()

		syncClusterResourceToSeed = g.Add(flow.Task{
			Name: "Syncing shoot cluster information to seed",
			Fn:   flow.TaskFn(botanist.SyncClusterResourceToSeed).RetryUntilTimeout(defaultInterval, defaultTimeout),
		})
		deployNamespace = g.Add(flow.Task{
			Name:         "Deploying Shoot namespace in Seed",
			Fn:           flow.TaskFn(botanist.DeployNamespace).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(syncClusterResourceToSeed),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying network policies",
			Fn:           flow.TaskFn(botanist.DeployNetworkPolicies).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deployNamespace),
		})
		deployCloudProviderSecret = g.Add(flow.Task{
			Name:         "Deploying cloud provider account secret",
			Fn:           flow.TaskFn(botanist.DeployCloudProviderSecret).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deployNamespace),
		})
		deployKubeAPIServerService = g.Add(flow.Task{
			Name:         "Deploying Kubernetes API server service in the Seed cluster",
			Fn:           flow.SimpleTaskFn(botanist.DeployKubeAPIServerService).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deployNamespace),
		})
		waitUntilKubeAPIServerServiceIsReady = g.Add(flow.Task{
			Name:         "Waiting until Kubernetes API server service in the Seed cluster has reported readiness",
			Fn:           flow.TaskFn(botanist.WaitUntilKubeAPIServerServiceIsReady),
			Dependencies: flow.NewTaskIDs(deployKubeAPIServerService),
		})
		deploySecrets = g.Add(flow.Task{
			Name:         "Deploying Shoot certificates / keys",
			Fn:           flow.TaskFn(botanist.DeploySecrets),
			Dependencies: flow.NewTaskIDs(deployNamespace),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying internal domain DNS record",
			Fn:           flow.TaskFn(botanist.DeployInternalDomainDNSRecord).DoIf(managedInternalDNS),
			Dependencies: flow.NewTaskIDs(waitUntilKubeAPIServerServiceIsReady),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying external domain DNS record",
			Fn:           flow.TaskFn(botanist.DeployExternalDomainDNSRecord).DoIf(managedExternalDNS),
			Dependencies: flow.NewTaskIDs(deployNamespace),
		})
		deployInfrastructure = g.Add(flow.Task{
			Name:         "Deploying Shoot infrastructure",
			Fn:           flow.TaskFn(botanist.DeployInfrastructure).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployCloudProviderSecret),
		})
		waitUntilInfrastructureReady = g.Add(flow.Task{
			Name:         "Waiting until shoot infrastructure has been reconciled",
			Fn:           flow.TaskFn(botanist.WaitUntilInfrastructureReady),
			Dependencies: flow.NewTaskIDs(deployInfrastructure),
		})
		deployBackupEntryInGarden = g.Add(flow.Task{
			Name: "Deploying backup entry",
			Fn:   flow.TaskFn(botanist.DeployBackupEntryInGarden).DoIf(allowBackup),
		})
		wailtUntilBackupEntryInGardenReconciled = g.Add(flow.Task{
			Name:         "Waiting until the backup entry has been reconciled",
			Fn:           flow.TaskFn(botanist.WaitUntilBackupEntryInGardenReconciled).DoIf(allowBackup),
			Dependencies: flow.NewTaskIDs(deployBackupEntryInGarden),
		})
		deployETCD = g.Add(flow.Task{
			Name:         "Deploying main and events etcd",
			Fn:           flow.TaskFn(botanist.DeployETCD).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployCloudProviderSecret, wailtUntilBackupEntryInGardenReconciled),
		})
		waitUntilEtcdReady = g.Add(flow.Task{
			Name:         "Waiting until main and event etcd report readiness",
			Fn:           flow.TaskFn(botanist.WaitUntilEtcdReady).SkipIf(hibernationEnabled),
			Dependencies: flow.NewTaskIDs(deployETCD),
		})
		deployControlPlane = g.Add(flow.Task{
			Name:         "Deploying shoot control plane components",
			Fn:           flow.TaskFn(botanist.DeployControlPlane).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployCloudProviderSecret, waitUntilInfrastructureReady),
		})
		waitUntilControlPlaneReady = g.Add(flow.Task{
			Name:         "Waiting until shoot control plane has been reconciled",
			Fn:           flow.TaskFn(botanist.WaitUntilControlPlaneReady),
			Dependencies: flow.NewTaskIDs(deployControlPlane),
		})
		createOrUpdateEtcdEncryptionConfiguration = g.Add(flow.Task{
			Name:         "Applying etcd encryption configuration",
			Fn:           flow.TaskFn(botanist.ApplyEncryptionConfiguration).DoIf(enableEtcdEncryption),
			Dependencies: flow.NewTaskIDs(deployNamespace),
		})
		deployKubeAPIServer = g.Add(flow.Task{
			Name:         "Deploying Kubernetes API server",
			Fn:           flow.SimpleTaskFn(botanist.DeployKubeAPIServer).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployETCD, waitUntilEtcdReady, waitUntilKubeAPIServerServiceIsReady, waitUntilControlPlaneReady, createOrUpdateEtcdEncryptionConfiguration),
		})
		waitUntilKubeAPIServerIsReady = g.Add(flow.Task{
			Name:         "Waiting until Kubernetes API server reports readiness",
			Fn:           flow.TaskFn(botanist.WaitUntilKubeAPIServerReady).SkipIf(hibernationEnabled),
			Dependencies: flow.NewTaskIDs(deployKubeAPIServer),
		})
		deployControlPlaneExposure = g.Add(flow.Task{
			Name:         "Deploying shoot control plane exposure components",
			Fn:           flow.TaskFn(botanist.DeployControlPlaneExposure).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(waitUntilKubeAPIServerIsReady),
		})
		waitUntilControlPlaneExposureReady = g.Add(flow.Task{
			Name:         "Waiting until Shoot control plane exposure has been reconciled",
			Fn:           flow.TaskFn(botanist.WaitUntilControlPlaneExposureReady),
			Dependencies: flow.NewTaskIDs(deployControlPlaneExposure),
		})
		initializeShootClients = g.Add(flow.Task{
			Name:         "Initializing connection to Shoot",
			Fn:           flow.SimpleTaskFn(botanist.InitializeShootClients).RetryUntilTimeout(defaultInterval, 2*time.Minute),
			Dependencies: flow.NewTaskIDs(waitUntilKubeAPIServerIsReady, waitUntilControlPlaneExposureReady),
		})
		_ = g.Add(flow.Task{
			Name:         "Rewriting Shoot secrets if EncryptionConfiguration has changed",
			Fn:           flow.TaskFn(botanist.RewriteShootSecretsIfEncryptionConfigurationChanged).DoIf(enableEtcdEncryption && !hibernationEnabled).RetryUntilTimeout(defaultInterval, 15*time.Minute),
			Dependencies: flow.NewTaskIDs(initializeShootClients, createOrUpdateEtcdEncryptionConfiguration),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying Kubernetes scheduler",
			Fn:           flow.SimpleTaskFn(botanist.DeployKubeScheduler).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, waitUntilKubeAPIServerIsReady),
		})
		deployKubeControllerManager = g.Add(flow.Task{
			Name:         "Deploying Kubernetes controller manager",
			Fn:           flow.SimpleTaskFn(botanist.DeployKubeControllerManager).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, deployCloudProviderSecret, waitUntilKubeAPIServerIsReady),
		})
		_ = g.Add(flow.Task{
			Name:         "Syncing shoot access credentials to project namespace in Garden",
			Fn:           flow.TaskFn(botanist.SyncShootCredentialsToGarden).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deploySecrets, initializeShootClients, deployKubeControllerManager),
		})
		computeShootOSConfig = g.Add(flow.Task{
			Name:         "Computing operating system specific configuration for shoot workers",
			Fn:           flow.TaskFn(botanist.ComputeShootOperatingSystemConfig).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients, waitUntilInfrastructureReady),
		})
		deployGardenerResourceManager = g.Add(flow.Task{
			Name:         "Deploying gardener-resource-manager",
			Fn:           flow.TaskFn(botanist.DeployGardenerResourceManager).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients),
		})
		deployNetworking = g.Add(flow.Task{
			Name:         "Deploying shoot network plugin",
			Fn:           flow.TaskFn(botanist.DeployNetwork).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deployGardenerResourceManager, computeShootOSConfig),
		})
		waitUntilNetworkIsReady = g.Add(flow.Task{
			Name:         "Waiting until shoot network plugin has been reconciled",
			Fn:           flow.TaskFn(botanist.WaitUntilNetworkIsReady),
			Dependencies: flow.NewTaskIDs(deployNetworking),
		})
		deployManagedResources = g.Add(flow.Task{
			Name:         "Deploying managed resources",
			Fn:           flow.TaskFn(botanist.DeployManagedResources).RetryUntilTimeout(defaultInterval, defaultTimeout).SkipIf(hibernationEnabled),
			Dependencies: flow.NewTaskIDs(deployGardenerResourceManager, computeShootOSConfig),
		})
		deployWorker = g.Add(flow.Task{
			Name:         "Configuring shoot worker pools",
			Fn:           flow.TaskFn(botanist.DeployWorker).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deployCloudProviderSecret, waitUntilInfrastructureReady, initializeShootClients, computeShootOSConfig),
		})
		waitUntilWorkerReady = g.Add(flow.Task{
			Name:         "Waiting until shoot worker nodes have been reconciled",
			Fn:           flow.TaskFn(botanist.WaitUntilWorkerReady),
			Dependencies: flow.NewTaskIDs(deployWorker),
		})
		_ = g.Add(flow.Task{
			Name:         "Ensuring ingress DNS record",
			Fn:           flow.TaskFn(botanist.EnsureIngressDNSRecord).DoIf(managedExternalDNS).RetryUntilTimeout(defaultInterval, 10*time.Minute),
			Dependencies: flow.NewTaskIDs(deployManagedResources),
		})
		waitUntilVPNConnectionExists = g.Add(flow.Task{
			Name:         "Waiting until the Kubernetes API server can connect to the Shoot workers",
			Fn:           flow.TaskFn(botanist.WaitUntilVPNConnectionExists).SkipIf(hibernationEnabled),
			Dependencies: flow.NewTaskIDs(deployManagedResources, waitUntilNetworkIsReady, waitUntilWorkerReady),
		})
		deploySeedMonitoring = g.Add(flow.Task{
			Name:         "Deploying Shoot monitoring stack in Seed",
			Fn:           flow.TaskFn(botanist.DeploySeedMonitoring).RetryUntilTimeout(defaultInterval, 2*time.Minute),
			Dependencies: flow.NewTaskIDs(waitUntilKubeAPIServerIsReady, initializeShootClients, waitUntilVPNConnectionExists, waitUntilWorkerReady),
		})
		deploySeedLogging = g.Add(flow.Task{
			Name:         "Deploying shoot logging stack in Seed",
			Fn:           flow.TaskFn(botanist.DeploySeedLogging).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(waitUntilKubeAPIServerIsReady, initializeShootClients, waitUntilVPNConnectionExists, waitUntilWorkerReady),
		})
		deployClusterAutoscaler = g.Add(flow.Task{
			Name:         "Deploying cluster autoscaler",
			Fn:           flow.TaskFn(botanist.DeployClusterAutoscaler).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(waitUntilWorkerReady, deployManagedResources, deploySeedMonitoring),
		})
		_ = g.Add(flow.Task{
			Name:         "Deploying Dependency Watchdog",
			Fn:           flow.TaskFn(botanist.DeployDependencyWatchdog).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(deployNamespace),
		})
		_ = g.Add(flow.Task{
			Name:         "Hibernating control plane",
			Fn:           flow.TaskFn(botanist.HibernateControlPlane).RetryUntilTimeout(defaultInterval, 2*time.Minute).DoIf(hibernationEnabled),
			Dependencies: flow.NewTaskIDs(initializeShootClients, deploySeedMonitoring, deploySeedLogging, deployClusterAutoscaler),
		})
		deployExtensionResources = g.Add(flow.Task{
			Name:         "Deploying extension resources",
			Fn:           flow.TaskFn(botanist.DeployExtensionResources).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients),
		})
		_ = g.Add(flow.Task{
			Name:         "Waiting until extension resources are ready",
			Fn:           flow.TaskFn(botanist.WaitUntilExtensionResourcesReady),
			Dependencies: flow.NewTaskIDs(deployExtensionResources),
		})
		deleteStaleExtensionResources = g.Add(flow.Task{
			Name:         "Delete stale extension resources",
			Fn:           flow.TaskFn(botanist.DeleteStaleExtensionResources).RetryUntilTimeout(defaultInterval, defaultTimeout),
			Dependencies: flow.NewTaskIDs(initializeShootClients),
		})
		_ = g.Add(flow.Task{
			Name:         "Waiting until stale extension resources are deleted",
			Fn:           flow.TaskFn(botanist.WaitUntilExtensionResourcesDeleted).SkipIf(hibernationEnabled),
			Dependencies: flow.NewTaskIDs(deleteStaleExtensionResources),
		})
	)
}

func (c *Controller) updateShootStatusReconcile(o *operation.Operation, operationType gardencorev1alpha1.LastOperationType, state gardencorev1alpha1.LastOperationState, retryCycleStartTime *metav1.Time) error {
	var (
		status             = o.Shoot.Info.Status
		now                = metav1.Now()
		observedGeneration = o.Shoot.Info.Generation
	)

	newShoot, err := kutil.TryUpdateShootStatus(c.k8sGardenClient.GardenCore(), retry.DefaultRetry, o.Shoot.Info.ObjectMeta,
		func(shoot *gardencorev1alpha1.Shoot) (*gardencorev1alpha1.Shoot, error) {
			if len(status.UID) == 0 {
				shoot.Status.UID = shoot.UID
			}
			if len(status.TechnicalID) == 0 {
				shoot.Status.TechnicalID = o.Shoot.SeedNamespace
			}
			if retryCycleStartTime != nil {
				shoot.Status.RetryCycleStartTime = retryCycleStartTime
			}

			shoot.Status.Gardener = *(o.GardenerInfo)
			shoot.Status.ObservedGeneration = observedGeneration
			shoot.Status.LastOperation = &gardencorev1alpha1.LastOperation{
				Type:           operationType,
				State:          state,
				Progress:       1,
				Description:    "Reconciliation of Shoot cluster state in progress.",
				LastUpdateTime: now,
			}
			return shoot, nil
		})
	if err == nil {
		o.Shoot.Info = newShoot
	}
	return err
}

func (c *Controller) updateShootStatusResetRetry(o *operation.Operation, operationType gardencorev1alpha1.LastOperationType) error {
	now := metav1.NewTime(time.Now().UTC())
	return c.updateShootStatusReconcile(o, operationType, gardencorev1alpha1.LastOperationStateError, &now)
}

func (c *Controller) updateShootStatusReconcileStart(o *operation.Operation, operationType gardencorev1alpha1.LastOperationType) error {
	var retryCycleStartTime *metav1.Time

	if o.Shoot.Info.Status.RetryCycleStartTime == nil ||
		o.Shoot.Info.Generation != o.Shoot.Info.Status.ObservedGeneration ||
		o.Shoot.Info.Status.Gardener.Version == version.Get().GitVersion ||
		(o.Shoot.Info.Status.LastOperation != nil && o.Shoot.Info.Status.LastOperation.State == gardencorev1alpha1.LastOperationStateFailed) {

		now := metav1.NewTime(time.Now().UTC())
		retryCycleStartTime = &now
	}

	return c.updateShootStatusReconcile(o, operationType, gardencorev1alpha1.LastOperationStateProcessing, retryCycleStartTime)
}

func (c *Controller) updateShootStatusReconcileSuccess(o *operation.Operation, operationType gardencorev1alpha1.LastOperationType) error {
	// Remove task list from Shoot annotations since reconciliation was successful.
	newShoot, err := kutil.TryUpdateShootAnnotations(c.k8sGardenClient.GardenCore(), retry.DefaultRetry, o.Shoot.Info.ObjectMeta,
		func(shoot *gardencorev1alpha1.Shoot) (*gardencorev1alpha1.Shoot, error) {
			controllerutils.RemoveAllTasks(shoot.Annotations)
			return shoot, nil
		})

	if err != nil {
		return err
	}

	newShoot, err = kutil.TryUpdateShootStatus(c.k8sGardenClient.GardenCore(), retry.DefaultRetry, newShoot.ObjectMeta,
		func(shoot *gardencorev1alpha1.Shoot) (*gardencorev1alpha1.Shoot, error) {
			shoot.Status.RetryCycleStartTime = nil
			shoot.Status.Seed = &o.Seed.Info.Name
			shoot.Status.IsHibernated = o.Shoot.HibernationEnabled
			shoot.Status.LastErrors = nil
			shoot.Status.LastError = nil
			shoot.Status.LastOperation = &gardencorev1alpha1.LastOperation{
				Type:           operationType,
				State:          gardencorev1alpha1.LastOperationStateSucceeded,
				Progress:       100,
				Description:    "Shoot cluster state has been successfully reconciled.",
				LastUpdateTime: metav1.Now(),
			}
			return shoot, nil
		})

	if err == nil {
		o.Shoot.Info = newShoot
	}
	return err
}

func (c *Controller) updateShootStatusReconcileError(o *operation.Operation, operationType gardencorev1alpha1.LastOperationType, description string, lastErrors ...gardencorev1alpha1.LastError) error {
	var (
		state         = gardencorev1alpha1.LastOperationStateFailed
		lastOperation = o.Shoot.Info.Status.LastOperation
		progress      = 1
		willRetry     = !utils.TimeElapsed(o.Shoot.Info.Status.RetryCycleStartTime, c.config.Controllers.Shoot.RetryDuration.Duration)
	)

	// TODO: Remove this after LastError is removed from the ShootStatus API
	var codes []gardencorev1alpha1.ErrorCode
	for _, lastErr := range lastErrors {
		codes = append(codes, lastErr.Codes...)
	}
	lastError := gardencorev1alpha1helper.LastError(description, codes...)

	newShoot, err := kutil.TryUpdateShootStatus(c.k8sGardenClient.GardenCore(), retry.DefaultRetry, o.Shoot.Info.ObjectMeta,
		func(shoot *gardencorev1alpha1.Shoot) (*gardencorev1alpha1.Shoot, error) {
			if willRetry {
				description += " Operation will be retried."
				state = gardencorev1alpha1.LastOperationStateError
			} else {
				shoot.Status.RetryCycleStartTime = nil
			}

			if lastOperation != nil {
				progress = lastOperation.Progress
			}

			shoot.Status.LastErrors = lastErrors

			// TODO: Remove this after LastError is removed from the ShootStatus API
			shoot.Status.LastError = lastError

			shoot.Status.LastOperation = &gardencorev1alpha1.LastOperation{
				Type:           operationType,
				State:          state,
				Progress:       progress,
				Description:    description,
				LastUpdateTime: metav1.Now(),
			}
			shoot.Status.Gardener = *(o.GardenerInfo)
			return shoot, nil
		})
	if err == nil {
		o.Shoot.Info = newShoot
	}

	newShootAfterLabel, err := kutil.TryUpdateShootLabels(c.k8sGardenClient.GardenCore(), retry.DefaultRetry, o.Shoot.Info.ObjectMeta, StatusLabelTransform(StatusUnhealthy))
	if err == nil {
		o.Shoot.Info = newShootAfterLabel
	}

	return err
}

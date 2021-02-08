// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package managedseed

import (
	"io/ioutil"
	"os"
	"strings"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	seedmanagementv1alpha1 "github.com/gardener/gardener/pkg/apis/seedmanagement/v1alpha1"
	"github.com/gardener/gardener/pkg/features"
	"github.com/gardener/gardener/pkg/gardenlet/apis/config"
	confighelper "github.com/gardener/gardener/pkg/gardenlet/apis/config/helper"
	configv1alpha1 "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
	gardenletfeatures "github.com/gardener/gardener/pkg/gardenlet/features"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	"github.com/gardener/gardener/pkg/utils/secrets"

	"github.com/Masterminds/semver"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/version"
)

// ValuesHelper provides methods for merging GardenletDeployment and GardenletConfiguration with parent,
// as well as computing the values to be used when applying the gardenlet chart.
type ValuesHelper interface {
	// MergeGardenletDeployment merges the given GardenletDeployment with the values from the parent gardenlet.
	MergeGardenletDeployment(*seedmanagementv1alpha1.GardenletDeployment, *gardencorev1beta1.Shoot) (*seedmanagementv1alpha1.GardenletDeployment, error)
	// MergeGardenletConfiguration merges the given GardenletConfiguration with the parent GardenletConfiguration.
	MergeGardenletConfiguration(config *configv1alpha1.GardenletConfiguration) (*configv1alpha1.GardenletConfiguration, error)
	// GetGardenletChartValues computes the values to be used when applying the gardenlet chart.
	GetGardenletChartValues(*seedmanagementv1alpha1.GardenletDeployment, *configv1alpha1.GardenletConfiguration, string, bool) (map[string]interface{}, error)
}

// valuesHelper is a concrete implementation of ValuesHelper
type valuesHelper struct {
	config      *config.GardenletConfiguration
	imageVector imagevector.ImageVector
}

// newValuesHelper creates a new ValuesHelper with the given parent GardenletConfiguration and image vector.
func newValuesHelper(config *config.GardenletConfiguration, imageVector imagevector.ImageVector) ValuesHelper {
	return &valuesHelper{
		config:      config,
		imageVector: imageVector,
	}
}

// MergeGardenletDeployment merges the given GardenletDeployment with the values from the parent gardenlet.
func (vp *valuesHelper) MergeGardenletDeployment(deployment *seedmanagementv1alpha1.GardenletDeployment, shoot *gardencorev1beta1.Shoot) (*seedmanagementv1alpha1.GardenletDeployment, error) {
	// Convert deployment object to values
	deploymentValues, err := utils.ToValuesMap(deployment)
	if err != nil {
		return nil, err
	}

	// Get parent deployment values
	parentDeployment, err := getParentGardenletDeployment(vp.imageVector, shoot)
	if err != nil {
		return nil, err
	}
	parentDeploymentValues, err := utils.ToValuesMap(parentDeployment)
	if err != nil {
		return nil, err
	}

	// Merge with parent
	deploymentValues = utils.MergeMaps(parentDeploymentValues, deploymentValues)

	// Convert deployment values back to an object
	var deploymentObj *seedmanagementv1alpha1.GardenletDeployment
	if err := utils.FromValuesMap(deploymentValues, &deploymentObj); err != nil {
		return nil, err
	}

	return deploymentObj, nil
}

// MergeGardenletConfiguration merges the given GardenletConfiguration with the parent GardenletConfiguration.
func (vp *valuesHelper) MergeGardenletConfiguration(config *configv1alpha1.GardenletConfiguration) (*configv1alpha1.GardenletConfiguration, error) {
	// Convert configuration object to values
	configValues, err := utils.ToValuesMap(config)
	if err != nil {
		return nil, err
	}

	// Get parent config values
	parentConfig, err := confighelper.ConvertGardenletConfigurationExternal(vp.config)
	if err != nil {
		return nil, err
	}
	parentConfigValues, err := utils.ToValuesMap(parentConfig)
	if err != nil {
		return nil, err
	}

	// Delete seedClientConnection.kubeconfig in parent config values
	parentConfigValues, err = utils.DeleteFromValuesMap(parentConfigValues, "seedClientConnection", "kubeconfig")
	if err != nil {
		return nil, err
	}

	// Merge with parent
	configValues = utils.MergeMaps(parentConfigValues, configValues)

	// Convert config values back to an object
	var configObj *configv1alpha1.GardenletConfiguration
	if err := utils.FromValuesMap(configValues, &configObj); err != nil {
		return nil, err
	}

	return configObj, nil
}

// GetGardenletChartValues computes the values to be used when applying the gardenlet chart.
func (vp *valuesHelper) GetGardenletChartValues(
	deployment *seedmanagementv1alpha1.GardenletDeployment,
	config *configv1alpha1.GardenletConfiguration,
	bootstrapKubeconfig string,
	mergeWithParent bool,
) (map[string]interface{}, error) {
	var err error

	// Get deployment values
	deploymentValues, err := vp.getGardenletDeploymentValues(deployment, mergeWithParent)
	if err != nil {
		return nil, err
	}

	// Get config values
	configValues, err := vp.getGardenletConfigurationValues(config, bootstrapKubeconfig)
	if err != nil {
		return nil, err
	}

	// Set gardenlet values to deployment values
	gardenletValues := deploymentValues

	// Ensure gardenlet values is a non-nil map
	gardenletValues = utils.InitValuesMap(gardenletValues)

	// Set config values in gardenlet values
	gardenletValues, err = utils.SetToValuesMap(gardenletValues, configValues, "config")
	if err != nil {
		return nil, err
	}

	// Return gardenlet chart values
	return map[string]interface{}{
		"global": map[string]interface{}{
			"gardenlet": gardenletValues,
		},
	}, nil
}

// getGardenletDeploymentValues computes and returns the gardenlet deployment values from the given GardenletDeployment.
func (vp *valuesHelper) getGardenletDeploymentValues(deployment *seedmanagementv1alpha1.GardenletDeployment, mergeWithParent bool) (map[string]interface{}, error) {
	// Convert deployment object to values
	deploymentValues, err := utils.ToValuesMap(deployment)
	if err != nil {
		return nil, err
	}

	if mergeWithParent {
		// Set imageVectorOverwrite and componentImageVectorOverwrites from parent
		deploymentValues["imageVectorOverwrite"], err = getParentImageVectorOverwrite()
		if err != nil {
			return nil, err
		}
		deploymentValues["componentImageVectorOverwrites"], err = getParentComponentImageVectorOverwrites()
		if err != nil {
			return nil, err
		}
	}

	return deploymentValues, nil
}

// getGardenletConfigurationValues computes and returns the gardenlet configuration values from the given GardenletConfiguration.
func (vp *valuesHelper) getGardenletConfigurationValues(config *configv1alpha1.GardenletConfiguration, bootstrapKubeconfig string) (map[string]interface{}, error) {
	// Convert configuration object to values
	configValues, err := utils.ToValuesMap(config)
	if err != nil {
		return nil, err
	}

	// If bootstrap kubeconfig is specified, set it in gardenClientConnection
	// Otherwise, if kubeconfig path is specified in gardenClientConnection, read it and store its contents
	if bootstrapKubeconfig != "" {
		configValues, err = utils.SetToValuesMap(configValues, bootstrapKubeconfig, "gardenClientConnection", "bootstrapKubeconfig", "kubeconfig")
		if err != nil {
			return nil, err
		}
	} else {
		kubeconfigPath, err := utils.GetFromValuesMap(configValues, "gardenClientConnection", "kubeconfig")
		if err != nil {
			return nil, err
		}
		if kubeconfigPath != nil && kubeconfigPath.(string) != "" {
			kubeconfig, err := ioutil.ReadFile(kubeconfigPath.(string))
			if err != nil {
				return nil, err
			}
			configValues, err = utils.SetToValuesMap(configValues, string(kubeconfig), "gardenClientConnection", "kubeconfig")
			if err != nil {
				return nil, err
			}
		}
	}

	// If kubeconfig path is specified in seedClientConnection, read it and store its contents
	kubeconfigPath, err := utils.GetFromValuesMap(configValues, "seedClientConnection", "kubeconfig")
	if err != nil {
		return nil, err
	}
	if kubeconfigPath != nil && kubeconfigPath.(string) != "" {
		kubeconfig, err := ioutil.ReadFile(kubeconfigPath.(string))
		if err != nil {
			return nil, err
		}
		configValues, err = utils.SetToValuesMap(configValues, string(kubeconfig), "seedClientConnection", "kubeconfig")
		if err != nil {
			return nil, err
		}
	}

	// Read server certificate file and store its contents
	certPath, err := utils.GetFromValuesMap(configValues, "server", "https", "tls", "serverCertPath")
	if err != nil {
		return nil, err
	}
	if certPath != nil && certPath.(string) != "" && !strings.Contains(certPath.(string), secrets.TemporaryDirectoryForSelfGeneratedTLSCertificatesPattern) {
		cert, err := ioutil.ReadFile(certPath.(string))
		if err != nil {
			return nil, err
		}
		configValues, err = utils.SetToValuesMap(configValues, string(cert), "server", "https", "tls", "crt")
		if err != nil {
			return nil, err
		}
	}

	// Read server key file and store its contents
	keyPath, err := utils.GetFromValuesMap(configValues, "server", "https", "tls", "serverKeyPath")
	if err != nil {
		return nil, err
	}
	if keyPath != nil && keyPath.(string) != "" && !strings.Contains(keyPath.(string), secrets.TemporaryDirectoryForSelfGeneratedTLSCertificatesPattern) {
		key, err := ioutil.ReadFile(keyPath.(string))
		if err != nil {
			return nil, err
		}
		configValues, err = utils.SetToValuesMap(configValues, string(key), "server", "https", "tls", "key")
		if err != nil {
			return nil, err
		}
	}

	// Delete server certificate and key paths
	configValues, err = utils.DeleteFromValuesMap(configValues, "server", "https", "tls", "serverCertPath")
	if err != nil {
		return nil, err
	}
	configValues, err = utils.DeleteFromValuesMap(configValues, "server", "https", "tls", "serverKeyPath")
	if err != nil {
		return nil, err
	}

	return configValues, nil
}

func getParentGardenletDeployment(imageVector imagevector.ImageVector, shoot *gardencorev1beta1.Shoot) (*seedmanagementv1alpha1.GardenletDeployment, error) {
	// Get image repository and tag
	var imageRepository, imageTag string
	gardenletImage, err := imageVector.FindImage("gardenlet")
	if err != nil {
		return nil, err
	}
	if gardenletImage.Tag != nil {
		imageRepository = gardenletImage.Repository
		imageTag = *gardenletImage.Tag
	} else {
		imageRepository = gardenletImage.String()
		imageTag = version.Get().GitVersion
	}

	// Create and return result
	return &seedmanagementv1alpha1.GardenletDeployment{
		Image: &seedmanagementv1alpha1.Image{
			Repository: &imageRepository,
			Tag:        &imageTag,
		},
		PodAnnotations: getParentPodAnnotations(shoot),
	}, nil
}

func getParentImageVectorOverwrite() (string, error) {
	var imageVectorOverwrite string
	if overWritePath := os.Getenv(imagevector.OverrideEnv); len(overWritePath) > 0 {
		data, err := ioutil.ReadFile(overWritePath)
		if err != nil {
			return "", err
		}
		imageVectorOverwrite = string(data)
	}
	return imageVectorOverwrite, nil
}

func getParentComponentImageVectorOverwrites() (string, error) {
	var componentImageVectorOverwrites string
	if overWritePath := os.Getenv(imagevector.ComponentOverrideEnv); len(overWritePath) > 0 {
		data, err := ioutil.ReadFile(overWritePath)
		if err != nil {
			return "", err
		}
		componentImageVectorOverwrites = string(data)
	}
	return componentImageVectorOverwrites, nil
}

var minimumAPIServerSNISidecarConstraint *semver.Constraints

func init() {
	var err error
	// 1.13.0-0 must be used or no 1.13.0-dev version can be matched
	minimumAPIServerSNISidecarConstraint, err = semver.NewConstraint(">= 1.13.0-0")
	utilruntime.Must(err)
}

func getParentPodAnnotations(shoot *gardencorev1beta1.Shoot) map[string]string {
	// If APIServerSNI is enabled for the seed cluster then the gardenlet must be restarted, so the Pod injector would
	// add `KUBERNETES_SERVICE_HOST` environment variable.
	if gardenletfeatures.FeatureGate.Enabled(features.APIServerSNI) {
		vers, err := semver.NewVersion(shoot.Status.Gardener.Version)
		if err != nil {
			// We can't really do anything in case of error, since it's not a transient error.
			// Returning an error would force another reconciliation that would fail again here.
			// Reconciling from this point makes no sense, unless the shoot is updated.
			return nil
		}
		if vers != nil && minimumAPIServerSNISidecarConstraint.Check(vers) {
			return map[string]string{
				"networking.gardener.cloud/seed-sni-enabled": "true",
			}
		}
	}
	return nil
}

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
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/client-go/restmapper"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/chartrenderer"
	gardencoreinformers "github.com/gardener/gardener/pkg/client/core/informers/externalversions/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/features"
	gardenletfeatures "github.com/gardener/gardener/pkg/gardenlet/features"
	"github.com/gardener/gardener/pkg/operation/common"
	"github.com/gardener/gardener/pkg/utils"
	chartutils "github.com/gardener/gardener/pkg/utils/chart"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	retryutils "github.com/gardener/gardener/pkg/utils/retry"
	secretsutils "github.com/gardener/gardener/pkg/utils/secrets"
	versionutils "github.com/gardener/gardener/pkg/utils/version"
	"github.com/gardener/gardener/pkg/version"

	resourcesv1alpha1 "github.com/gardener/gardener-resource-manager/pkg/apis/resources/v1alpha1"
	resourcehealth "github.com/gardener/gardener-resource-manager/pkg/health"
	"github.com/gardener/gardener-resource-manager/pkg/manager"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/util/retry"
	componentbaseconfig "k8s.io/component-base/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	caSeed = "ca-seed"
	// ManagedResourceLabelKeyOrigin is a key for a injected label on managed resources for Seeds with the value 'origin'.
	ManagedResourceLabelKeyOrigin = "origin"
	// ManagedResourceLabelValueGardener is a value for a injected label on managed resources for Seeds with the value 'gardener'.
	ManagedResourceLabelValueGardener = "gardener"
)

var wantedCertificateAuthorities = map[string]*secretsutils.CertificateSecretConfig{
	caSeed: {
		Name:       caSeed,
		CommonName: "kubernetes",
		CertType:   secretsutils.CACert,
	},
}

// New takes a <k8sGardenClient>, the <k8sGardenCoreInformers> and a <seed> manifest, and creates a new Seed representation.
// It will add the CloudProfile and identify the cloud provider.
func New(seed *gardencorev1beta1.Seed) *Seed {
	seedObj := &Seed{Info: seed}

	return seedObj
}

// NewFromName creates a new Seed object based on the name of a Seed manifest.
func NewFromName(k8sGardenCoreInformers gardencoreinformers.Interface, seedName string) *Seed {
	seed, err := k8sGardenCoreInformers.Seeds().Lister().Get(seedName)
	if err != nil {
		return nil
	}
	return New(seed)
}

// List returns a list of Seed clusters (along with the referenced secrets).
func List(k8sGardenCoreInformers gardencoreinformers.Interface) ([]*Seed, error) {
	var seedList []*Seed

	list, err := k8sGardenCoreInformers.Seeds().Lister().List(labels.Everything())
	if err != nil {
		return nil, err
	}

	for _, obj := range list {
		seedList = append(seedList, New(obj))
	}

	return seedList, nil
}

// InitializeClients initializes Seed.K8sClient, Seed.ChartRenderer and Seed.ChartApplier by retrieving the seed secret
// from the garden client, if `inCluster` equals false, or by using the in-cluster config otherwise.
func (s *Seed) InitializeClients(ctx context.Context, gardenClient client.Client, clientConnectionConfig componentbaseconfig.ClientConnectionConfiguration, inCluster bool) error {
	k8sSeedClient, err := GetSeedClient(ctx, gardenClient, clientConnectionConfig, inCluster, s.Info.Name)
	if err != nil {
		return err
	}
	s.K8sClient = k8sSeedClient

	renderer, err := chartrenderer.NewForConfig(k8sSeedClient.RESTConfig())
	if err != nil {
		return err
	}
	s.ChartRenderer = renderer

	applier, err := kubernetes.NewApplierForConfig(k8sSeedClient.RESTConfig())
	if err != nil {
		return err
	}

	s.ChartApplier = kubernetes.NewChartApplier(renderer, applier)

	return nil
}

// GetSeedClient returns the Kubernetes client for the seed cluster. If `inCluster` is set to true then
// the in-cluster client is returned, otherwise the secret reference of the given `seedName` is read
// and a client with the stored kubeconfig is created.
func GetSeedClient(ctx context.Context, gardenClient client.Client, clientConnection componentbaseconfig.ClientConnectionConfiguration, inCluster bool, seedName string) (kubernetes.Interface, error) {
	if inCluster {
		return kubernetes.NewClientFromFile(
			"",
			clientConnection.Kubeconfig,
			kubernetes.WithClientConnectionOptions(clientConnection),
			kubernetes.WithClientOptions(
				client.Options{
					Scheme: kubernetes.SeedScheme,
				},
			),
			kubernetes.WithDeferredDiscoveryRESTMapper(),
		)
	}

	seed := &gardencorev1beta1.Seed{}
	if err := gardenClient.Get(ctx, kutil.Key(seedName), seed); err != nil {
		return nil, err
	}

	if seed.Spec.SecretRef == nil {
		return nil, fmt.Errorf("seed has no secret reference pointing to a kubeconfig - cannot create client")
	}

	seedSecret, err := common.GetSecretFromSecretRef(ctx, gardenClient, seed.Spec.SecretRef)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewClientFromSecretObject(
		seedSecret,
		kubernetes.WithClientConnectionOptions(clientConnection),
		kubernetes.WithClientOptions(client.Options{
			Scheme: kubernetes.SeedScheme,
		}),
		kubernetes.WithDeferredDiscoveryRESTMapper(),
	)
}

const (
	grafanaPrefix = "g-seed"
	grafanaTLS    = "grafana-tls"

	prometheusPrefix = "p-seed"
	prometheusTLS    = "aggregate-prometheus-tls"

	kibanaPrefix = "k-seed"
	kibanaTLS    = "kibana-tls"

	vpaTLS = "vpa-tls-certs"
)

// readExistingSecrets reads existing Kubernetes Secrets from the Seed cluster in the garden namespace and returns
// them in a map with the Secrets' names as the keys.
func readExistingSecrets(ctx context.Context, c client.Client) (map[string]*corev1.Secret, error) {
	var (
		existingSecrets    = &corev1.SecretList{}
		existingSecretsMap = map[string]*corev1.Secret{}
	)
	if err := c.List(ctx, existingSecrets, client.InNamespace(v1beta1constants.GardenNamespace)); err != nil {
		return nil, err
	}

	for _, secret := range existingSecrets.Items {
		secretObj := secret
		existingSecretsMap[secret.ObjectMeta.Name] = &secretObj
	}

	return existingSecretsMap, nil
}

// computeWantedSecrets returns a list of Secret configuration objects satisfying the secret config interface,
// each containing their specific configuration for the creation of certificates (server/client), RSA key pairs, basic
// authentication credentials, etc.
func computeWantedSecrets(seed *Seed, certificateAuthorities map[string]*secretsutils.Certificate) ([]secretsutils.ConfigInterface, error) {
	if len(certificateAuthorities) != len(wantedCertificateAuthorities) {
		return nil, fmt.Errorf("missing certificate authorities")
	}

	endUserCrtValidity := common.EndUserCrtValidity

	secretList := []secretsutils.ConfigInterface{
		&secretsutils.CertificateSecretConfig{
			Name: vpaTLS,

			CommonName:   "vpa-webhook.garden.svc",
			Organization: nil,
			DNSNames:     []string{"vpa-webhook.garden.svc", "vpa-webhook"},
			IPAddresses:  nil,

			CertType:  secretsutils.ServerCert,
			SigningCA: certificateAuthorities[caSeed],
		},
		&secretsutils.CertificateSecretConfig{
			Name: common.GrafanaTLS,

			CommonName:   "grafana",
			Organization: []string{"garden.sapcloud.io:monitoring:ingress"},
			DNSNames:     []string{seed.GetIngressFQDN(grafanaPrefix)},
			IPAddresses:  nil,

			CertType:  secretsutils.ServerCert,
			SigningCA: certificateAuthorities[caSeed],
			Validity:  &endUserCrtValidity,
		},
		&secretsutils.CertificateSecretConfig{
			Name: prometheusTLS,

			CommonName:   "prometheus",
			Organization: []string{"garden.sapcloud.io:monitoring:ingress"},
			DNSNames:     []string{seed.GetIngressFQDN(prometheusPrefix)},
			IPAddresses:  nil,

			CertType:  secretsutils.ServerCert,
			SigningCA: certificateAuthorities[caSeed],
			Validity:  &endUserCrtValidity,
		},
	}

	// Logging feature gate
	if gardenletfeatures.FeatureGate.Enabled(features.Logging) {
		secretList = append(secretList,
			&secretsutils.CertificateSecretConfig{
				Name: kibanaTLS,

				CommonName:   "kibana",
				Organization: []string{"garden.sapcloud.io:logging:ingress"},
				DNSNames:     []string{seed.GetIngressFQDN(kibanaPrefix)},
				IPAddresses:  nil,

				CertType:  secretsutils.ServerCert,
				SigningCA: certificateAuthorities[caSeed],
				Validity:  &endUserCrtValidity,
			},

			// Secret definition for logging ingress
			&secretsutils.BasicAuthSecretConfig{
				Name:   common.SeedLoggingIngressCredentialsSecretName,
				Format: secretsutils.BasicAuthFormatNormal,

				Username:       "admin",
				PasswordLength: 32,
			},
			&secretsutils.BasicAuthSecretConfig{
				Name:   common.FluentdCredentialsSecretName,
				Format: secretsutils.BasicAuthFormatNormal,

				Username:                  "fluentd",
				PasswordLength:            32,
				BcryptPasswordHashRequest: true,
			},
		)
	}

	return secretList, nil
}

// generateSeedSecrets generates CA, TLS and auth secrets inside the garden namespace
// It takes a map[string]*corev1.Secret object which contains secrets that have already been deployed in the Seed to avoid duplication errors.
func generateSeedSecrets(ctx context.Context, seed *Seed) (map[string]*corev1.Secret, error) {
	// read existing and generate wanted secrets
	existingSecretsMap, err := readExistingSecrets(ctx, seed.K8sClient.Client())
	if err != nil {
		return nil, fmt.Errorf("failed to read existing Seed secrets: %w", err)
	}

	allSeedSecrets := map[string]*corev1.Secret{}

	// generate CAs
	caSecrets, certificateAuthorities, err := secretsutils.GenerateCertificateAuthorities(existingSecretsMap, wantedCertificateAuthorities, v1beta1constants.GardenNamespace)
	if err != nil {
		return nil, err
	}

	for name, secret := range caSecrets {
		allSeedSecrets[name] = secret
	}

	// generate cluster secrets
	wantedSecretsList, err := computeWantedSecrets(seed, certificateAuthorities)
	if err != nil {
		return nil, err
	}

	clusterSecrets, err := secretsutils.GenerateClusterSecrets(existingSecretsMap, wantedSecretsList, v1beta1constants.GardenNamespace)
	if err != nil {
		return nil, err
	}

	for name, secret := range clusterSecrets {
		allSeedSecrets[name] = secret
	}

	return allSeedSecrets, nil
}

const chartNameSeedBootstrap = "seed-bootstrap"

// BootstrapCluster bootstraps a Seed cluster and deploys a managed resource containing various required manifests.
func BootstrapCluster(ctx context.Context, seed *Seed, logger logrus.FieldLogger, gardenSecrets map[string]*corev1.Secret, imageVector imagevector.ImageVector, componentImageVectors imagevector.ComponentImageVectors) error {
	gardenNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: v1beta1constants.GardenNamespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, seed.K8sClient.Client(), gardenNamespace, func() error {
		kutil.SetMetaDataLabel(&gardenNamespace.ObjectMeta, "role", v1beta1constants.GardenNamespace)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to create/update %s namespace", v1beta1constants.GardenNamespace)
	}

	if _, err := kutil.TryUpdateNamespaceLabels(seed.K8sClient.Kubernetes(), retry.DefaultBackoff, metav1.ObjectMeta{Name: metav1.NamespaceSystem}, func(ns *corev1.Namespace) (*corev1.Namespace, error) {
		kutil.SetMetaDataLabel(&ns.ObjectMeta, "role", metav1.NamespaceSystem)
		return ns, nil
	}); err != nil {
		return fmt.Errorf("failed to update %s namespace", metav1.NamespaceSystem)
	}

	logger.Infof("Deploying %s chart", chartNameSeedResourceManager)
	if err := deploySeedResourceManager(ctx, seed.ChartApplier, imageVector); err != nil {
		return fmt.Errorf("failed to deploy %s chart: %+v", chartNameSeedResourceManager, err)
	}

	values, err := computeSeedBootstrapValues(ctx, seed, gardenSecrets, imageVector, componentImageVectors)
	if err != nil {
		return fmt.Errorf("failed to compute values for %s chart: %+v", chartNameSeedResourceManager, err)
	}

	release, err := seed.ChartRenderer.Render(filepath.Join("charts", "seed-bootstrap"), chartNameSeedBootstrap, v1beta1constants.GardenNamespace, values)
	if err != nil {
		return fmt.Errorf("failed to render %s chart: %+v", chartNameSeedBootstrap, err)
	}

	// Create secret
	data := make(map[string][]byte, len(release.Files()))
	for fileName, fileContent := range release.Files() {
		key := strings.Replace(fileName, "/", "_", -1)
		data[key] = []byte(fileContent)
	}

	logger.Infof("Deploying secret for managed resource '%s/%s'", v1beta1constants.GardenNamespace, chartNameSeedResourceManager)
	if err := manager.NewSecret(seed.K8sClient.Client()).
		WithNamespacedName(v1beta1constants.GardenNamespace, chartNameSeedBootstrap).
		WithLabels(map[string]string{v1beta1constants.GardenRole: v1beta1constants.GardenRoleSeed}).
		WithKeyValues(data).
		Reconcile(ctx); err != nil {
		return fmt.Errorf("failed to reconcile secret '%s/%s' for ManagedResource: %+v", v1beta1constants.GardenNamespace, chartNameSeedBootstrap, err)
	}

	logger.Infof("Deploying managed resource '%s/%s'", v1beta1constants.GardenNamespace, chartNameSeedResourceManager)
	// retry on NoMatchError (CRD might take a few seconds to get ready)
	if err = retry.OnError(retry.DefaultRetry, func(err error) bool {
		if meta.IsNoMatchError(err) {
			// reset rest mapper, CRD might have just been created
			if restMapper, ok := seed.K8sClient.RESTMapper().(*restmapper.DeferredDiscoveryRESTMapper); ok {
				restMapper.Reset()
				return true
			}
			return false
		}
		return false
	}, func() error {
		return manager.NewManagedResource(seed.K8sClient.Client()).
			WithNamespacedName(v1beta1constants.GardenNamespace, chartNameSeedBootstrap).
			WithLabels(map[string]string{v1beta1constants.GardenRole: v1beta1constants.GardenRoleSeed}).
			WithInjectedLabels(map[string]string{ManagedResourceLabelKeyOrigin: ManagedResourceLabelValueGardener}).
			WithSecretRef(chartNameSeedBootstrap).
			WithClass(v1beta1constants.SeedResourceManagerClass).
			Delete
			Reconcile(ctx)
	}); err != nil {
		return fmt.Errorf("failed to reconcile ManagedResource '%s/%s': %+v", v1beta1constants.GardenNamespace, chartNameSeedBootstrap, err)
	}

	return nil
}

func computeSeedBootstrapValues(ctx context.Context, seed *Seed, gardenSecrets map[string]*corev1.Secret, imageVector imagevector.ImageVector, componentImageVectors imagevector.ComponentImageVectors) (map[string]interface{}, error) {
	// base values
	values := map[string]interface{}{
		"cloudProvider": seed.Info.Spec.Provider.Type,
		"reserveExcessCapacity": map[string]interface{}{
			"enabled":  seed.reserveExcessCapacity,
			"replicas": DesiredExcessCapacity(),
		},
		"hvpa": map[string]interface{}{
			"enabled": gardenletfeatures.FeatureGate.Enabled(features.HVPA),
		},
	}

	// images
	imagesValues, err := computeSeedBootstrapImagesValues(seed, imageVector, componentImageVectors)
	if err != nil {
		return nil, fmt.Errorf("failed to compute images values: %w", err)
	}
	values = utils.MergeMaps(values, imagesValues)

	// certs and secrets
	seedSecrets, err := generateSeedSecrets(ctx, seed)
	if err != nil {
		return nil, err
	}

	values = utils.MergeMaps(values, map[string]interface{}{
		"secrets": chartutils.SecretMapToValues(seedSecrets),
		"vpa": map[string]interface{}{
			"podAnnotations": map[string]interface{}{
				"checksum/secret-vpa-tls-certs": common.ComputeSecretCheckSum(seedSecrets[vpaTLS].Data),
			},
		},
	})

	var (
		grafanaTLSSecretName    = grafanaTLS
		prometheusTLSSecretName = prometheusTLS
		kibanaTLSSecretName     = kibanaTLS
	)
	wildcardCert, err := GetWildcardCertificate(ctx, seed.K8sClient.Client())
	if err != nil {
		return nil, err
	}

	if wildcardCert != nil {
		grafanaTLSSecretName = wildcardCert.GetName()
		prometheusTLSSecretName = wildcardCert.GetName()
		kibanaTLSSecretName = wildcardCert.GetName()
	}

	// networks
	networkPoliciesValues, err := computeSeedNetworkPoliciesValues(seed)
	if err != nil {
		return nil, fmt.Errorf("failed computing Seed network policies values: %w", err)
	}
	values = utils.MergeMaps(values, networkPoliciesValues)

	// logging
	// TODO (tim-ebert): move to separate chart
	loggingValues, err := computeSeedLoggingValues(ctx, seed, seedSecrets, kibanaTLSSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed computing Seed logging values: %w", err)
	}
	values = utils.MergeMaps(values, loggingValues)

	// =======================
	// monitoring
	// =======================
	// TODO (tim-ebert): move to separate chart
	monitoringValues, err := computeSeedMonitoringValues(seed, gardenSecrets, prometheusTLSSecretName, grafanaTLSSecretName)
	if err != nil {
		return nil, fmt.Errorf("failed computing Seed monitoring values: %w", err)
	}
	values = utils.MergeMaps(values, monitoringValues)

	return values, nil
}

func computeSeedBootstrapImagesValues(seed *Seed, imageVector imagevector.ImageVector, componentImageVectors imagevector.ComponentImageVectors) (map[string]interface{}, error) {
	images, err := imagevector.FindImages(imageVector,
		[]string{
			common.AlertManagerImageName,
			common.AlpineImageName,
			common.ConfigMapReloaderImageName,
			common.CuratorImageName,
			common.ElasticsearchImageName,
			common.ElasticsearchMetricsExporterImageName,
			common.FluentBitImageName,
			common.FluentdEsImageName,
			common.GrafanaImageName,
			common.KibanaImageName,
			common.PauseContainerImageName,
			common.PrometheusImageName,
			common.VpaAdmissionControllerImageName,
			common.VpaExporterImageName,
			common.VpaRecommenderImageName,
			common.VpaUpdaterImageName,
			common.HvpaControllerImageName,
			common.DependencyWatchdogImageName,
			common.KubeStateMetricsImageName,
			common.EtcdDruidImageName,
		},
		imagevector.RuntimeVersion(seed.K8sClient.Version()),
		imagevector.TargetVersion(seed.K8sClient.Version()),
	)
	if err != nil {
		return nil, err
	}

	// Special handling for gardener-seed-admission-controller because it's a component whose version is controlled by
	// this project/repository
	gardenerSeedAdmissionControllerImage, err := imageVector.FindImage(common.GardenerSeedAdmissionControllerImageName)
	if err != nil {
		return nil, err
	}
	var (
		repository = gardenerSeedAdmissionControllerImage.String()
		tag        = version.Get().GitVersion
	)
	if gardenerSeedAdmissionControllerImage.Tag != nil {
		repository = gardenerSeedAdmissionControllerImage.Repository
		tag = *gardenerSeedAdmissionControllerImage.Tag
	}
	images[common.GardenerSeedAdmissionControllerImageName] = &imagevector.Image{
		Repository: repository,
		Tag:        &tag,
	}

	imageVectorOverwrites := map[string]interface{}{}
	for name, data := range componentImageVectors {
		imageVectorOverwrites[name] = data
	}

	return map[string]interface{}{
		"global": map[string]interface{}{
			"images":                chartutils.ImageMapToValues(images),
			"imageVectorOverwrites": imageVectorOverwrites,
		},
	}, nil
}

func computeSeedNetworkPoliciesValues(seed *Seed) (map[string]interface{}, error) {
	networks := []string{
		seed.Info.Spec.Networks.Pods,
		seed.Info.Spec.Networks.Services,
	}
	if v := seed.Info.Spec.Networks.Nodes; v != nil {
		networks = append(networks, *v)
	}

	privateNetworks, err := common.ToExceptNetworks(common.AllPrivateNetworkBlocks(), networks...)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"global-network-policies": map[string]interface{}{
			"denyAll":         false,
			"privateNetworks": privateNetworks,
		},
	}, nil
}

func computeSeedLoggingValues(ctx context.Context, seed *Seed, seedSecrets map[string]*corev1.Secret, kibanaTLSSecretName string) (map[string]interface{}, error) {
	var (
		loggingEnabled        = gardenletfeatures.FeatureGate.Enabled(features.Logging)
		nodeCount             int
		basicAuth             string
		kibanaHost            string
		sgFluentdPassword     string
		sgFluentdPasswordHash string
		filters               = strings.Builder{}
		parsers               = strings.Builder{}
	)

	if loggingEnabled {
		nodes := &corev1.NodeList{}
		if err := seed.K8sClient.Client().List(ctx, nodes); err != nil {
			return nil, err
		}
		nodeCount = len(nodes.Items)

		credentials := seedSecrets[common.SeedLoggingIngressCredentialsSecretName]
		basicAuth = utils.CreateSHA1Secret(credentials.Data[secretsutils.DataKeyUserName], credentials.Data[secretsutils.DataKeyPassword])
		kibanaHost = seed.GetIngressFQDN(kibanaPrefix)

		sgFluentdCredentials := seedSecrets[common.FluentdCredentialsSecretName]
		sgFluentdPassword = string(sgFluentdCredentials.Data[secretsutils.DataKeyPassword])
		sgFluentdPasswordHash = string(sgFluentdCredentials.Data[secretsutils.DataKeyPasswordBcryptHash])

		// Read extension provider specific configuration
		existingConfigMaps := &corev1.ConfigMapList{}
		if err := seed.K8sClient.Client().List(ctx, existingConfigMaps,
			client.InNamespace(v1beta1constants.GardenNamespace),
			client.MatchingLabels{v1beta1constants.LabelExtensionConfiguration: v1beta1constants.LabelLogging}); err != nil {
			return nil, err
		}

		// Read all filters and parsers coming from the extension provider configurations
		for _, cm := range existingConfigMaps.Items {
			filters.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.FluentBitConfigMapKubernetesFilter]))
			parsers.WriteString(fmt.Sprintln(cm.Data[v1beta1constants.FluentBitConfigMapParser]))
		}
	} else {
		// TODO (tim-ebert): ensure that all logging PVCs are deleted
		//if err := common.DeleteLoggingStack(context.TODO(), seed.K8sClient.Client(), v1beta1constants.GardenNamespace); err != nil && !apierrors.IsNotFound(err) {
		//	return err
		//}
	}

	return map[string]interface{}{
		"elastic-kibana-curator": map[string]interface{}{
			"enabled": loggingEnabled,
			"ingress": map[string]interface{}{
				"basicAuthSecret": basicAuth,
				"hosts": []map[string]interface{}{
					{
						"hostName":   kibanaHost,
						"secretName": kibanaTLSSecretName,
					},
				},
			},
			"curator": map[string]interface{}{
				// Set curator threshold to 5Gi
				"diskSpaceThreshold": 5 * 1024 * 1024 * 1024,
			},
			"elasticsearch": map[string]interface{}{
				"objectCount": nodeCount,
				"persistence": map[string]interface{}{
					"size": seed.GetValidVolumeSize("100Gi"),
				},
			},
			"searchguard": map[string]interface{}{
				"users": map[string]interface{}{
					"fluentd": map[string]interface{}{
						"hash": sgFluentdPasswordHash,
					},
				},
			},
		},
		"fluentd-es": map[string]interface{}{
			"enabled": loggingEnabled,
			"fluentd": map[string]interface{}{
				"sgUsername": "fluentd",
				"sgPassword": sgFluentdPassword,
				"storage":    seed.GetValidVolumeSize("9Gi"),
			},
			"fluentbit": map[string]interface{}{
				"extensions": map[string]interface{}{
					"parsers": parsers.String(),
					"filters": filters.String(),
				},
			},
		},
	}, nil
}

func computeSeedMonitoringValues(seed *Seed, gardenSecrets map[string]*corev1.Secret, prometheusTLSSecretName, grafanaTLSSecretName string) (map[string]interface{}, error) {
	var (
		monitoringIngressCredentials map[string][]byte
		monitoringIngressBasicAuth   string
	)

	if globalMonitoringSecret, ok := gardenSecrets[common.GardenRoleGlobalMonitoring]; ok {
		monitoringIngressCredentials = globalMonitoringSecret.Data
		monitoringIngressBasicAuth = utils.CreateSHA1Secret(globalMonitoringSecret.Data[secretsutils.DataKeyUserName], globalMonitoringSecret.Data[secretsutils.DataKeyPassword])
	}

	// AlertManager configuration
	alertManagerValues := map[string]interface{}{
		"storage": seed.GetValidVolumeSize("1Gi"),
	}

	alertingSMTPKeys := common.GetSecretKeysWithPrefix(common.GardenRoleAlerting, gardenSecrets)

	if seedWantsAlertmanager(alertingSMTPKeys, gardenSecrets) {
		emailConfigs := make([]map[string]interface{}, 0, len(alertingSMTPKeys))
		for _, key := range alertingSMTPKeys {
			if string(gardenSecrets[key].Data["auth_type"]) == "smtp" {
				secret := gardenSecrets[key]
				emailConfigs = append(emailConfigs, map[string]interface{}{
					"to":            string(secret.Data["to"]),
					"from":          string(secret.Data["from"]),
					"smarthost":     string(secret.Data["smarthost"]),
					"auth_username": string(secret.Data["auth_username"]),
					"auth_identity": string(secret.Data["auth_identity"]),
					"auth_password": string(secret.Data["auth_password"]),
				})
				alertManagerValues["enabled"] = true
				alertManagerValues["emailConfigs"] = emailConfigs
				break
			}
		}
	} else {
		// TODO (tim-ebert): ensure that all alertmanager resources are deleted
		alertManagerValues["enabled"] = false
	}

	return map[string]interface{}{
		"prometheus": map[string]interface{}{
			"storage": seed.GetValidVolumeSize("10Gi"),
		},
		"aggregatePrometheus": map[string]interface{}{
			"storage":    seed.GetValidVolumeSize("20Gi"),
			"seed":       seed.Info.Name,
			"hostName":   seed.GetIngressFQDN(prometheusPrefix),
			"secretName": prometheusTLSSecretName,
		},
		"grafana": map[string]interface{}{
			"hostName":   seed.GetIngressFQDN(grafanaPrefix),
			"secretName": grafanaTLSSecretName,
		},
		"alertmanager": alertManagerValues,
		"monitoring": map[string]interface{}{
			"ingress": map[string]interface{}{
				"basicAuth":   monitoringIngressBasicAuth,
				"credentials": monitoringIngressCredentials,
			},
		},
	}, nil
}

func CleanupSeed(ctx context.Context, seed *Seed, logger logrus.FieldLogger, reportMsg func(string)) error {
	// TODO (tim-ebert): protect seed-bootstrap mr from accidental deletion
	// TODO (tim-ebert): ensure that all PVCs are deleted

	logger.Infof("Deleting managed resource '%s/%s'", v1beta1constants.GardenNamespace, chartNameSeedBootstrap)
	if err := manager.NewManagedResource(seed.K8sClient.Client()).
		WithNamespacedName(v1beta1constants.GardenNamespace, chartNameSeedBootstrap).
		Delete(ctx); client.IgnoreNotFound(err) != nil {
		return err
	}

	if err := waitUntilSeedBootstrapManagedResourcesDeleted(ctx, seed, logger, reportMsg); err != nil {
		return err
	}
	logger.Infof("Successfully deleted managed resource '%s/%s'", v1beta1constants.GardenNamespace, chartNameSeedBootstrap)

	logger.Infof("Deleting secret of managed resource '%s/%s'", v1beta1constants.GardenNamespace, chartNameSeedBootstrap)
	if err := manager.NewSecret(seed.K8sClient.Client()).
		WithNamespacedName(v1beta1constants.GardenNamespace, chartNameSeedBootstrap).
		Delete(ctx); client.IgnoreNotFound(err) != nil {
		return err
	}

	logger.Infof("Deleting %s chart", chartNameSeedResourceManager)
	if err := deleteSeedResourceManager(ctx, seed.ChartApplier); err != nil {
		return err
	}

	return nil
}

func WaitUntilSeedBootstrapped(ctx context.Context, seed *Seed, logger logrus.FieldLogger, reportMsg func(string)) error {
	return retryutils.UntilTimeout(ctx, 5*time.Second, 5*time.Minute, func(ctx context.Context) (bool, error) {
		managedResources := &resourcesv1alpha1.ManagedResourceList{}
		if err := seed.K8sClient.Client().List(ctx,
			managedResources,
			client.InNamespace(v1beta1constants.GardenNamespace),
			client.MatchingLabels{v1beta1constants.GardenRole: v1beta1constants.GardenRoleSeed}); err != nil {
			if meta.IsNoMatchError(err) {
				return retryutils.MinorError(err)
			}
			return retryutils.SevereError(err)
		}

		var unreadyResources []string
		for _, mr := range managedResources.Items {
			if err2 := resourcehealth.CheckManagedResourceApplied(&mr); err2 != nil {
				unreadyResources = append(unreadyResources, mr.Namespace+"/"+mr.Name)
			}
		}
		if len(unreadyResources) == 0 {
			return retryutils.Ok()
		}

		logger.Infof("Waiting until all managed resources of seed bootstrapping have been applied. Pending resources: %v", unreadyResources)
		msg := fmt.Sprintf("not all managed resources of seed bootstrapping have been applied: %v", unreadyResources)
		reportMsg(msg)
		return retryutils.MinorError(errors.New(msg))
	})
}

func waitUntilSeedBootstrapManagedResourcesDeleted(ctx context.Context, seed *Seed, logger logrus.FieldLogger, reportMsg func(string)) error {
	return retryutils.UntilTimeout(ctx, 5*time.Second, 10*time.Minute, func(ctx context.Context) (bool, error) {
		managedResources := &resourcesv1alpha1.ManagedResourceList{}
		if err := seed.K8sClient.Client().List(ctx,
			managedResources,
			client.InNamespace(v1beta1constants.GardenNamespace),
			client.MatchingLabels{v1beta1constants.GardenRole: v1beta1constants.GardenRoleSeed}); err != nil {
			return retryutils.SevereError(err)
		}

		if len(managedResources.Items) == 0 {
			return retryutils.Ok()
		}

		var pendingResources []string
		for _, mr := range managedResources.Items {
			pendingResources = append(pendingResources, mr.Namespace+"/"+mr.Name)
		}

		logger.Infof("Waiting until all managed resources of seed bootstrapping have been deleted. Pending resources: %v", pendingResources)
		msg := fmt.Sprintf("not all managed resources of seed bootstrapping have been deleted: %v", pendingResources)
		reportMsg(msg)
		return retryutils.MinorError(errors.New(msg))
	})
}

const chartNameSeedResourceManager = "seed-resource-manager"

func deploySeedResourceManager(ctx context.Context, chartApplier kubernetes.ChartApplier, imageVector imagevector.ImageVector) error {
	values, err := computeSeedResourceManagerValues(imageVector)
	if err != nil {
		return err
	}

	return chartApplier.Apply(ctx, filepath.Join("charts", chartNameSeedResourceManager), v1beta1constants.GardenNamespace, chartNameSeedResourceManager, values, kubernetes.DefaultMergeFuncs)
}

func deleteSeedResourceManager(ctx context.Context, chartApplier kubernetes.ChartApplier) error {
	return chartApplier.Delete(ctx, filepath.Join("charts", chartNameSeedResourceManager), v1beta1constants.GardenNamespace, chartNameSeedResourceManager)
}

func computeSeedResourceManagerValues(imageVector imagevector.ImageVector) (kubernetes.ValueOption, error) {
	image, err := imageVector.FindImage(common.GardenerResourceManagerImageName)
	if err != nil {
		return nil, err
	}

	return kubernetes.Values(map[string]interface{}{
		"images": map[string]interface{}{
			"gardener-resource-manager": image.String(),
		},
		"gardenerResourceManager": map[string]interface{}{
			"resourceClass": v1beta1constants.SeedResourceManagerClass,
		},
	}), nil
}

// DesiredExcessCapacity computes the required resources (CPU and memory) required to deploy new shoot control planes
// (on the seed) in terms of reserve-excess-capacity deployment replicas. Each deployment replica currently
// corresponds to resources of (request/limits) 2 cores of CPU and 6Gi of RAM.
// This roughly corresponds to a single, moderately large control-plane.
// The logic for computation of desired excess capacity corresponds to deploying 2 such shoot control planes.
// This excess capacity can be used for hosting new control planes or newly vertically scaled old control-planes.
func DesiredExcessCapacity() int {
	var (
		replicasToSupportSingleShoot = 1
		effectiveExcessCapacity      = 2
	)

	return effectiveExcessCapacity * replicasToSupportSingleShoot
}

// GetIngressFQDNDeprecated returns the fully qualified domain name of ingress sub-resource for the Seed cluster. The
// end result is '<subDomain>.<shootName>.<projectName>.<seed-ingress-domain>'.
// Only necessary to renew certificates for Alertmanager, Grafana, Kibana, Prometheus
// TODO: (timuthy) remove in future version.
func (s *Seed) GetIngressFQDNDeprecated(subDomain, shootName, projectName string) string {
	if shootName == "" {
		return fmt.Sprintf("%s.%s.%s", subDomain, projectName, s.Info.Spec.DNS.IngressDomain)
	}
	return fmt.Sprintf("%s.%s.%s.%s", subDomain, shootName, projectName, s.Info.Spec.DNS.IngressDomain)
}

// GetIngressFQDN returns the fully qualified domain name of ingress sub-resource for the Seed cluster. The
// end result is '<subDomain>.<shootName>.<projectName>.<seed-ingress-domain>'.
func (s *Seed) GetIngressFQDN(subDomain string) string {
	return fmt.Sprintf("%s.%s", subDomain, s.Info.Spec.DNS.IngressDomain)
}

// CheckMinimumK8SVersion checks whether the Kubernetes version of the Seed cluster fulfills the minimal requirements.
func (s *Seed) CheckMinimumK8SVersion() (string, error) {
	// We require CRD status subresources for the extension controllers that we install into the seeds.
	minSeedVersion := "1.11"

	version := s.K8sClient.Version()

	seedVersionOK, err := versionutils.CompareVersions(version, ">=", minSeedVersion)
	if err != nil {
		return "<unknown>", err
	}
	if !seedVersionOK {
		return "<unknown>", fmt.Errorf("the Kubernetes version of the Seed cluster must be at least %s", minSeedVersion)
	}
	return version, nil
}

// MustReserveExcessCapacity configures whether we have to reserve excess capacity in the Seed cluster.
func (s *Seed) MustReserveExcessCapacity(must bool) {
	s.reserveExcessCapacity = must
}

// GetValidVolumeSize is to get a valid volume size.
// If the given size is smaller than the minimum volume size permitted by cloud provider on which seed cluster is running, it will return the minimum size.
func (s *Seed) GetValidVolumeSize(size string) string {
	if s.Info.Spec.Volume == nil || s.Info.Spec.Volume.MinimumSize == nil {
		return size
	}

	qs, err := resource.ParseQuantity(size)
	if err == nil && qs.Cmp(*s.Info.Spec.Volume.MinimumSize) < 0 {
		return s.Info.Spec.Volume.MinimumSize.String()
	}

	return size
}

func seedWantsAlertmanager(keys []string, secrets map[string]*corev1.Secret) bool {
	for _, key := range keys {
		if string(secrets[key].Data["auth_type"]) == "smtp" {
			return true
		}
	}
	return false
}

// GetWildcardCertificate gets the wildcard certificate for the seed's ingress domain.
// Nil is returned if no wildcard certificate is configured.
func GetWildcardCertificate(ctx context.Context, c client.Client) (*corev1.Secret, error) {
	wildcardCerts := &corev1.SecretList{}
	if err := c.List(
		ctx,
		wildcardCerts,
		client.InNamespace(v1beta1constants.GardenNamespace),
		client.MatchingLabels(map[string]string{v1beta1constants.GardenRole: common.ControlPlaneWildcardCert}),
	); err != nil {
		return nil, err
	}

	if len(wildcardCerts.Items) > 1 {
		return nil, fmt.Errorf("misconfigured seed cluster: not possible to provide more than one secret with annotation %s", common.ControlPlaneWildcardCert)
	}

	if len(wildcardCerts.Items) == 1 {
		return &wildcardCerts.Items[0], nil
	}
	return nil, nil
}

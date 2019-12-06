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

package operation

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"github.com/gardener/gardener/pkg/utils/flow"
	"net/http"

	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1alpha1helper "github.com/gardener/gardener/pkg/apis/core/v1alpha1/helper"
	gardencoreinformers "github.com/gardener/gardener/pkg/client/core/informers/externalversions/core/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/controllermanager/apis/config"
	"github.com/gardener/gardener/pkg/operation/garden"
	"github.com/gardener/gardener/pkg/operation/seed"
	"github.com/gardener/gardener/pkg/operation/shoot"
	"github.com/gardener/gardener/pkg/utils/imagevector"

	prometheusapi "github.com/prometheus/client_golang/api"
	prometheusclient "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

var _ = OperationInterface(&Operation{})

// Operation contains all data required to perform an operation on a Shoot cluster.
type Operation struct {
	Config                    *config.ControllerManagerConfiguration
	Logger                    *logrus.Entry
	GardenerInfo              *gardencorev1alpha1.Gardener
	Secrets                   map[string]*corev1.Secret
	CheckSums                 map[string]string
	ImageVector               imagevector.ImageVector
	Garden                    *garden.Garden
	Seed                      *seed.Seed
	Shoot                     *shoot.Shoot
	ShootedSeed               *gardencorev1alpha1helper.ShootedSeed
	K8sGardenClient           kubernetes.Interface
	K8sGardenCoreInformers    gardencoreinformers.Interface
	K8sSeedClient             kubernetes.Interface
	K8sShootClient            kubernetes.Interface
	ChartApplierGarden        kubernetes.ChartApplier
	ChartApplierSeed          kubernetes.ChartApplier
	ChartApplierShoot         kubernetes.ChartApplier
	APIServerAddress          string
	APIServerHealthCheckToken string
	SeedNamespaceObject       *corev1.Namespace
	ShootBackup               *config.ShootBackup
	MonitoringClient          prometheusclient.API
}

func (o *Operation) IsShootHibernationEnabled() bool {
	return o.Shoot.HibernationEnabled
}

func (o *Operation) IsShootExternalDomainManaged() bool {
	return o.Shoot.ExternalDomain != nil && o.Shoot.ExternalDomain.Provider != "unmanaged"
}

func (o *Operation) IsGardenInternalDomainManaged() bool {
	return o.Garden.InternalDomain != nil && o.Garden.InternalDomain.Provider != "unmanaged"
}

func (o *Operation) IsSeedBackupEnabled() bool {
	return o.Seed.Info.Spec.Backup != nil
}

type prometheusRoundTripper struct {
	authHeader string
	ca         *x509.CertPool
}

func (r prometheusRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", r.authHeader)
	prometheusapi.DefaultRoundTripper.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: r.ca}
	return prometheusapi.DefaultRoundTripper.RoundTrip(req)
}

type OperationInterface interface {
	InitializeSeedClients() error
	InitializeShootClients() error
	InitializeMonitoringClient() error
	ApplyChartGarden(chartPath, namespace, name string, defaultValues, additionalValues map[string]interface{}) error
	ApplyChartSeed(chartPath, namespace, name string, defaultValues, additionalValues map[string]interface{}) error
	GetSecretKeysOfRole(kind string) []string
	ReportShootProgress(ctx context.Context, stats *flow.Stats)
	CleanShootTaskError(ctx context.Context, taskID string)
	SeedVersion() string
	ShootVersion() string
	InjectSeedSeedImages(values map[string]interface{}, names ...string) (map[string]interface{}, error)
	InjectSeedShootImages(values map[string]interface{}, names ...string) (map[string]interface{}, error)
	InjectShootShootImages(values map[string]interface{}, names ...string) (map[string]interface{}, error)
	SyncClusterResourceToSeed(ctx context.Context) error
	DeleteClusterResourceFromSeed(ctx context.Context) error
	ComputeGrafanaHosts() []string
	ComputeGrafanaOperatorsHost() string
	ComputeGrafanaUsersHost() string
	ComputeAlertManagerHost() string
	ComputePrometheusHost() string
	ComputeKibanaHost() string
	ComputeIngressHost(prefix string) string

	OperationInformation
}

type OperationInformation interface {
	IsShootHibernationEnabled() bool
	IsShootExternalDomainManaged() bool
	IsGardenInternalDomainManaged() bool
	IsSeedBackupEnabled() bool
}

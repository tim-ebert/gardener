// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package fake

import (
	"github.com/gardener/gardener/pkg/chartrenderer"
	gardencoreclientset "github.com/gardener/gardener/pkg/client/core/clientset/versioned"
	"github.com/gardener/gardener/pkg/client/kubernetes"

	apiextensionclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	kubernetesclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	apiregistrationclientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClientSetBuilder is a builder for fake ClientSets
type ClientSetBuilder struct {
	applier         kubernetes.Applier
	chartRenderer   chartrenderer.Interface
	chartApplier    kubernetes.ChartApplier
	restConfig      *rest.Config
	client          client.Client
	directClient    client.Client
	cache           cache.Cache
	restMapper      meta.RESTMapper
	kubernetes      kubernetesclientset.Interface
	gardenCore      gardencoreclientset.Interface
	apiextension    apiextensionclientset.Interface
	apiregistration apiregistrationclientset.Interface
	restClient      rest.Interface
	version         string
}

// NewClientSetBuilder return a new builder for building fake ClientSets
func NewClientSetBuilder() *ClientSetBuilder {
	return &ClientSetBuilder{}
}

// WithApplier sets the applier attribute of the builder.
func (b *ClientSetBuilder) WithApplier(applier kubernetes.Applier) *ClientSetBuilder {
	b.applier = applier
	return b
}

// WithChartRenderer sets the chartRenderer attribute of the builder.
func (b *ClientSetBuilder) WithChartRenderer(chartRenderer chartrenderer.Interface) *ClientSetBuilder {
	b.chartRenderer = chartRenderer
	return b
}

// WithChartApplier sets the chartApplier attribute of the builder.
func (b *ClientSetBuilder) WithChartApplier(chartApplier kubernetes.ChartApplier) *ClientSetBuilder {
	b.chartApplier = chartApplier
	return b
}

// WithRESTConfig sets the restConfig attribute of the builder.
func (b *ClientSetBuilder) WithRESTConfig(config *rest.Config) *ClientSetBuilder {
	b.restConfig = config
	return b
}

// WithClient sets the client attribute of the builder.
func (b *ClientSetBuilder) WithClient(client client.Client) *ClientSetBuilder {
	b.client = client
	return b
}

// WithDirectClient sets the directClient attribute of the builder.
func (b *ClientSetBuilder) WithDirectClient(directClient client.Client) *ClientSetBuilder {
	b.directClient = directClient
	return b
}

// WithCache sets the cache attribute of the builder.
func (b *ClientSetBuilder) WithCache(cache cache.Cache) *ClientSetBuilder {
	b.cache = cache
	return b
}

// WithRESTMapper sets the restMapper attribute of the builder.
func (b *ClientSetBuilder) WithRESTMapper(restMapper meta.RESTMapper) *ClientSetBuilder {
	b.restMapper = restMapper
	return b
}

// WithKubernetes sets the kubernetes attribute of the builder.
func (b *ClientSetBuilder) WithKubernetes(kubernetes kubernetesclientset.Interface) *ClientSetBuilder {
	b.kubernetes = kubernetes
	return b
}

// WithGardenCore sets the gardenCore attribute of the builder.
func (b *ClientSetBuilder) WithGardenCore(gardenCore gardencoreclientset.Interface) *ClientSetBuilder {
	b.gardenCore = gardenCore
	return b
}

// WithAPIExtension sets the apiextension attribute of the builder.
func (b *ClientSetBuilder) WithAPIExtension(apiextension apiextensionclientset.Interface) *ClientSetBuilder {
	b.apiextension = apiextension
	return b
}

// WithAPIRegistration sets the apiregistration attribute of the builder.
func (b *ClientSetBuilder) WithAPIRegistration(apiregistration apiregistrationclientset.Interface) *ClientSetBuilder {
	b.apiregistration = apiregistration
	return b
}

// WithRESTClient sets the restClient attribute of the builder.
func (b *ClientSetBuilder) WithRESTClient(restClient rest.Interface) *ClientSetBuilder {
	b.restClient = restClient
	return b
}

// WithVersion sets the version attribute of the builder.
func (b *ClientSetBuilder) WithVersion(version string) *ClientSetBuilder {
	b.version = version
	return b
}

// Build builds the ClientSet.
func (b *ClientSetBuilder) Build() *ClientSet {
	return &ClientSet{
		applier:         b.applier,
		chartRenderer:   b.chartRenderer,
		chartApplier:    b.chartApplier,
		restConfig:      b.restConfig,
		client:          b.client,
		directClient:    b.directClient,
		cache:           b.cache,
		restMapper:      b.restMapper,
		kubernetes:      b.kubernetes,
		gardenCore:      b.gardenCore,
		apiextension:    b.apiextension,
		apiregistration: b.apiregistration,
		restClient:      b.restClient,
		version:         b.version,
	}
}

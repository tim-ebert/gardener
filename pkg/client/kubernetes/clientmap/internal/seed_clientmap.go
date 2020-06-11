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

package internal

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	baseconfig "k8s.io/component-base/config"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
)

type seedClientMap struct {
	clientmap.ClientMap
}

func NewSeedClientMap(factory *SeedClientSetFactory, logger logrus.FieldLogger) clientmap.ClientMap {
	return &seedClientMap{
		ClientMap: NewGenericClientMap(factory, logger),
	}
}

type SeedClientSetFactory struct {
	GetGardenClient        func(ctx context.Context) (kubernetes.Interface, error)
	InCluster              bool
	ClientConnectionConfig baseconfig.ClientConnectionConfiguration
}

func (f *SeedClientSetFactory) NewClientSet(ctx context.Context, k clientmap.ClientSetKey) (kubernetes.Interface, error) {
	key, ok := k.(SeedClientSetKey)
	if !ok {
		return nil, fmt.Errorf("call to GetClient with unsupported ClientSetKey: expected %T got %T", SeedClientSetKey(""), k)
	}

	if f.InCluster {
		clientSet, err := NewClientFromFile(
			"",
			f.ClientConnectionConfig.Kubeconfig,
			kubernetes.WithClientConnectionOptions(f.ClientConnectionConfig),
			kubernetes.WithClientOptions(
				client.Options{
					Scheme: kubernetes.SeedScheme,
				},
			),
		)
		if err != nil {
			return nil, fmt.Errorf("failed constructing new client for Seed cluster %q: %w", key.Key(), err)
		}

		return clientSet, nil
	}

	gardenClient, err := f.GetGardenClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get garden client: %w", err)
	}

	seed := &gardencorev1beta1.Seed{}
	if err := gardenClient.Client().Get(ctx, client.ObjectKey{Name: key.Key()}, seed); err != nil {
		return nil, fmt.Errorf("failed to get Seed object %q: %w", key.Key(), err)
	}

	if seed.Spec.SecretRef == nil {
		return nil, fmt.Errorf("seed %q does not have a secretRef", key.Key())
	}

	clientSet, err := NewClientFromSecret(ctx, gardenClient, seed.Spec.SecretRef.Namespace, seed.Spec.SecretRef.Name,
		kubernetes.WithClientConnectionOptions(f.ClientConnectionConfig),
		kubernetes.WithClientOptions(client.Options{
			Scheme: kubernetes.SeedScheme,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed constructing new client for Seed cluster %q: %w", key.Key(), err)
	}

	return clientSet, nil
}

type SeedClientSetKey string

func (k SeedClientSetKey) Key() string {
	return string(k)
}

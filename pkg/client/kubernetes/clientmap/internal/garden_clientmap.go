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
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
)

type gardenClientMap struct {
	clientmap.ClientMap
}

func NewGardenClientMap(factory *GardenClientSetFactory, logger logrus.FieldLogger) clientmap.ClientMap {
	return &gardenClientMap{
		ClientMap: NewGenericClientMap(factory, logger),
	}
}

type GardenClientSetFactory struct {
	RESTConfig *rest.Config
}

func (f *GardenClientSetFactory) NewClientSet(_ context.Context, k clientmap.ClientSetKey) (kubernetes.Interface, error) {
	_, ok := k.(GardenClientSetKey)
	if !ok {
		return nil, fmt.Errorf("call to GetClient with unsupported ClientSetKey: expected %T got %T", GardenClientSetKey{}, k)
	}

	clientSet, err := NewClientSetWithConfig(
		kubernetes.WithRESTConfig(f.RESTConfig),
		kubernetes.WithClientOptions(client.Options{
			Scheme: kubernetes.GardenScheme,
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("failed constructing new client for Garden cluster: %w", err)
	}

	return clientSet, nil
}

type GardenClientSetKey struct{}

func (k GardenClientSetKey) Key() string {
	return "garden"
}

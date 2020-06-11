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

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
)

var _ clientmap.ClientMap = &DelegatingClientMap{}

type DelegatingClientMap struct {
	GardenClients clientmap.ClientMap
	SeedClients   clientmap.ClientMap
	ShootClients  clientmap.ClientMap
	PlantClients  clientmap.ClientMap
}

func NewDelegatingClientMap(gardenClientMap, seedClientMap, shootClientMap, plantClientMap clientmap.ClientMap) clientmap.ClientMap {
	return &DelegatingClientMap{
		GardenClients: gardenClientMap,
		SeedClients:   seedClientMap,
		ShootClients:  shootClientMap,
		PlantClients:  plantClientMap,
	}
}

type ErrUnsupportedKeyType struct {
	calledFunc string
	key        clientmap.ClientSetKey
}

func (e *ErrUnsupportedKeyType) Error() string {
	return fmt.Sprintf("call to %s with unsupported ClientSetKey type: %T", e.calledFunc, e.key)
}

func (cm *DelegatingClientMap) GetClient(ctx context.Context, key clientmap.ClientSetKey) (kubernetes.Interface, error) {
	switch key.(type) {
	case GardenClientSetKey:
		return cm.GardenClients.GetClient(ctx, key)
	case SeedClientSetKey:
		return cm.SeedClients.GetClient(ctx, key)
	case ShootClientSetKey:
		return cm.ShootClients.GetClient(ctx, key)
	case PlantClientSetKey:
		return cm.PlantClients.GetClient(ctx, key)
	}

	return nil, &ErrUnsupportedKeyType{
		calledFunc: "GetClient",
		key:        key,
	}
}

func (cm *DelegatingClientMap) InvalidateClient(key clientmap.ClientSetKey) error {
	switch key.(type) {
	case GardenClientSetKey:
		return cm.GardenClients.InvalidateClient(key)
	case SeedClientSetKey:
		return cm.SeedClients.InvalidateClient(key)
	case ShootClientSetKey:
		return cm.ShootClients.InvalidateClient(key)
	case PlantClientSetKey:
		return cm.PlantClients.InvalidateClient(key)
	}

	return &ErrUnsupportedKeyType{
		calledFunc: "InvalidateClient",
		key:        key,
	}
}

func (cm *DelegatingClientMap) Start(stopCh <-chan struct{}) error {
	if err := cm.GardenClients.Start(stopCh); err != nil {
		return fmt.Errorf("failed to start garden ClientMap: %w", err)
	}

	if cm.SeedClients != nil {
		if err := cm.SeedClients.Start(stopCh); err != nil {
			return fmt.Errorf("failed to start seed ClientMap: %w", err)
		}
	}

	if cm.ShootClients != nil {
		if err := cm.ShootClients.Start(stopCh); err != nil {
			return fmt.Errorf("failed to start shoot ClientMap: %w", err)
		}
	}

	if cm.PlantClients != nil {
		if err := cm.PlantClients.Start(stopCh); err != nil {
			return fmt.Errorf("failed to start plant ClientMap: %w", err)
		}
	}
	return nil
}

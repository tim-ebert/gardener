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

package builder

import (
	"fmt"

	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap/internal"

	"github.com/sirupsen/logrus"
)

type DelegatingClientMapBuilder struct {
	gardenClientMapFunc func(logger logrus.FieldLogger) (clientmap.ClientMap, error)
	seedClientMapFunc   func(gardenClients clientmap.ClientMap, logger logrus.FieldLogger) (clientmap.ClientMap, error)
	shootClientMapFunc  func(gardenClients, seedClients clientmap.ClientMap, logger logrus.FieldLogger) (clientmap.ClientMap, error)
	plantClientMapFunc  func(gardenClients clientmap.ClientMap, logger logrus.FieldLogger) (clientmap.ClientMap, error)

	logger logrus.FieldLogger
}

func NewDelegatingClientMapBuilder() *DelegatingClientMapBuilder {
	return &DelegatingClientMapBuilder{
		gardenClientMapFunc: func(_ logrus.FieldLogger) (clientmap.ClientMap, error) {
			return nil, fmt.Errorf("garden ClientMap is required but not set")
		},
	}
}

// WithLogger sets the logger attribute of the builder.
func (b *DelegatingClientMapBuilder) WithLogger(logger logrus.FieldLogger) *DelegatingClientMapBuilder {
	b.logger = logger
	return b
}

// WithGardenClientMap sets the gardenClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithGardenClientMap(clientMap clientmap.ClientMap) *DelegatingClientMapBuilder {
	b.gardenClientMapFunc = func(_ logrus.FieldLogger) (clientmap.ClientMap, error) {
		return clientMap, nil
	}
	return b
}

// WithGardenClientMapBuilder sets the gardenClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithGardenClientMapBuilder(builder *GardenClientMapBuilder) *DelegatingClientMapBuilder {
	b.gardenClientMapFunc = func(logger logrus.FieldLogger) (clientmap.ClientMap, error) {
		return builder.
			WithLogger(logger.WithField("ClientMap", "GardenClientMap")).
			Build()
	}
	return b
}

// WithSeedClientMap sets the seedClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithSeedClientMap(clientMap clientmap.ClientMap) *DelegatingClientMapBuilder {
	b.seedClientMapFunc = func(_ clientmap.ClientMap, _ logrus.FieldLogger) (clientmap.ClientMap, error) {
		return clientMap, nil
	}
	return b
}

// WithSeedClientMapBuilder sets the seedClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithSeedClientMapBuilder(builder *SeedClientMapBuilder) *DelegatingClientMapBuilder {
	b.seedClientMapFunc = func(gardenClients clientmap.ClientMap, logger logrus.FieldLogger) (clientmap.ClientMap, error) {
		return builder.
			WithLogger(logger.WithField("ClientMap", "SeedClientMap")).
			WithGardenClientMap(gardenClients).
			Build()
	}
	return b
}

// WithShootClientMap sets the shootClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithShootClientMap(clientMap clientmap.ClientMap) *DelegatingClientMapBuilder {
	b.shootClientMapFunc = func(_, _ clientmap.ClientMap, _ logrus.FieldLogger) (clientmap.ClientMap, error) {
		return clientMap, nil
	}
	return b
}

// WithShootClientMapBuilder sets the shootClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithShootClientMapBuilder(builder *ShootClientMapBuilder) *DelegatingClientMapBuilder {
	b.shootClientMapFunc = func(gardenClients, seedClients clientmap.ClientMap, logger logrus.FieldLogger) (clientmap.ClientMap, error) {
		return builder.
			WithLogger(logger.WithField("ClientMap", "ShootClientMap")).
			WithGardenClientMap(gardenClients).
			WithSeedClientMap(seedClients).
			Build()
	}
	return b
}

// WithPlantClientMap sets the plantClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithPlantClientMap(clientMap clientmap.ClientMap) *DelegatingClientMapBuilder {
	b.plantClientMapFunc = func(_ clientmap.ClientMap, _ logrus.FieldLogger) (clientmap.ClientMap, error) {
		return clientMap, nil
	}
	return b
}

// WithPlantClientMapBuilder sets the plantClientMap attribute of the builder.
func (b *DelegatingClientMapBuilder) WithPlantClientMapBuilder(builder *PlantClientMapBuilder) *DelegatingClientMapBuilder {
	b.plantClientMapFunc = func(gardenClients clientmap.ClientMap, logger logrus.FieldLogger) (clientmap.ClientMap, error) {
		return builder.
			WithLogger(logger.WithField("ClientMap", "PlantClientMap")).
			WithGardenClientMap(gardenClients).
			Build()
	}
	return b
}

func (b *DelegatingClientMapBuilder) Build() (clientmap.ClientMap, error) {
	if b.logger == nil {
		return nil, fmt.Errorf("logger is required but not set")
	}

	gardenClients, err := b.gardenClientMapFunc(b.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to construct garden ClientMap: %w", err)
	}

	var seedClients, shootClients, plantClients clientmap.ClientMap

	if b.seedClientMapFunc != nil {
		seedClients, err = b.seedClientMapFunc(gardenClients, b.logger)
		if err != nil {
			return nil, fmt.Errorf("failed to construct seed ClientMap: %w", err)
		}
	}

	if b.shootClientMapFunc != nil {
		if seedClients == nil {
			return nil, fmt.Errorf("failed to construct shoot ClientMap, seed ClientMap is required but not set")
		}

		shootClients, err = b.shootClientMapFunc(gardenClients, seedClients, b.logger)
		if err != nil {
			return nil, fmt.Errorf("failed to construct shoot ClientMap: %w", err)
		}
	}

	if b.plantClientMapFunc != nil {
		plantClients, err = b.plantClientMapFunc(gardenClients, b.logger)
		if err != nil {
			return nil, fmt.Errorf("failed to construct plant ClientMap: %w", err)
		}
	}

	return internal.NewDelegatingClientMap(gardenClients, seedClients, shootClients, plantClients), nil
}

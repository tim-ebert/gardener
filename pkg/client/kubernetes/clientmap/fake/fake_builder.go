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
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	"github.com/gardener/gardener/pkg/client/kubernetes/fake"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClientMapBuilder struct {
	clientSets map[clientmap.ClientSetKey]kubernetes.Interface
}

func NewClientMapBuilder() *ClientMapBuilder {
	return &ClientMapBuilder{
		clientSets: make(map[clientmap.ClientSetKey]kubernetes.Interface),
	}
}

func (b *ClientMapBuilder) WithClientSets(clientSets map[clientmap.ClientSetKey]kubernetes.Interface) *ClientMapBuilder {
	b.clientSets = clientSets
	return b
}

func (b *ClientMapBuilder) WithClientSetForKey(key clientmap.ClientSetKey, cs kubernetes.Interface) *ClientMapBuilder {
	b.clientSets[key] = cs
	return b
}

func (b *ClientMapBuilder) WithRuntimeClientForKey(key clientmap.ClientSetKey, c client.Client) *ClientMapBuilder {
	b.clientSets[key] = fake.NewClientSetBuilder().WithClient(c).Build()
	return b
}

func (b *ClientMapBuilder) Build() *ClientMap {
	return NewClientMapWithClientSets(b.clientSets)
}

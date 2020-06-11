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

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	shootpkg "github.com/gardener/gardener/pkg/operation/shoot"

	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	baseconfig "k8s.io/component-base/config"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type shootClientMap struct {
	clientmap.ClientMap
}

func NewShootClientMap(factory *ShootClientSetFactory, logger logrus.FieldLogger) clientmap.ClientMap {
	return &shootClientMap{
		ClientMap: NewGenericClientMap(factory, logger),
	}
}

type ShootClientSetFactory struct {
	GetGardenClient        func(ctx context.Context) (kubernetes.Interface, error)
	GetSeedClient          func(ctx context.Context, name string) (kubernetes.Interface, error)
	ClientConnectionConfig baseconfig.ClientConnectionConfiguration

	Log logrus.FieldLogger
}

func (f *ShootClientSetFactory) NewClientSet(ctx context.Context, k clientmap.ClientSetKey) (kubernetes.Interface, error) {
	key, ok := k.(ShootClientSetKey)
	if !ok {
		return nil, fmt.Errorf("call to GetClient with unsupported ClientSetKey: expected %T got %T", ShootClientSetKey{}, k)
	}

	gardenClient, err := f.GetGardenClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get garden client: %w", err)
	}

	shoot := &gardencorev1beta1.Shoot{}
	if err := gardenClient.Client().Get(ctx, client.ObjectKey{Namespace: key.Namespace, Name: key.Name}, shoot); err != nil {
		return nil, fmt.Errorf("failed to get Shoot object %q: %w", key.Key(), err)
	}

	if shoot.Spec.SeedName == nil {
		return nil, fmt.Errorf("shoot %q is not scheduled yet", key.Key())
	}

	seedClient, err := f.GetSeedClient(ctx, *shoot.Spec.SeedName)
	if err != nil {
		return nil, fmt.Errorf("failed to get seed client: %w", err)
	}

	project, err := ProjectForNamespaceWithClient(ctx, gardenClient.Client(), shoot.Namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get Project for Shoot %q: %w", key.Key(), err)
	}

	seedNamespace := shootpkg.ComputeTechnicalID(project.Name, shoot)

	secretName := v1beta1constants.SecretNameGardener
	// If the gardenlet runs in the same cluster like the API server of the shoot then use the internal kubeconfig
	// and communicate internally. Otherwise, fall back to the "external" kubeconfig and communicate via the
	// load balancer of the shoot API server.
	addr, err := LookupHost(fmt.Sprintf("%s.%s.svc", v1beta1constants.DeploymentNameKubeAPIServer, seedNamespace))
	if err != nil {
		f.Log.Warnf("service DNS name lookup of kube-apiserver failed (%+v), falling back to external kubeconfig", err)
	} else if len(addr) > 0 {
		secretName = v1beta1constants.SecretNameGardenerInternal
	}

	clientOptions := client.Options{
		Scheme: kubernetes.ShootScheme,
	}

	clientSet, err := NewClientFromSecret(ctx, seedClient, seedNamespace, secretName,
		kubernetes.WithClientConnectionOptions(f.ClientConnectionConfig),
		kubernetes.WithClientOptions(clientOptions),
	)

	if secretName == v1beta1constants.SecretNameGardenerInternal && err != nil && apierrors.IsNotFound(err) {
		clientSet, err = NewClientFromSecret(ctx, seedClient, seedNamespace, v1beta1constants.SecretNameGardener,
			kubernetes.WithClientConnectionOptions(f.ClientConnectionConfig),
			kubernetes.WithClientOptions(clientOptions),
		)
	}

	if err != nil {
		return nil, fmt.Errorf("failed constructing new client for Shoot cluster %q: %w", key.Key(), err)
	}

	return clientSet, nil
}

type ShootClientSetKey struct {
	Namespace, Name string
}

func (k ShootClientSetKey) Key() string {
	return k.Namespace + "/" + k.Name
}

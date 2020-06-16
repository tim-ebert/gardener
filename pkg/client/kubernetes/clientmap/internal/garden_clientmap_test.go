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

package internal_test

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap/internal"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap/keys"
	fakeclientset "github.com/gardener/gardener/pkg/client/kubernetes/fake"
	"github.com/gardener/gardener/pkg/logger"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/client-go/rest"
)

var _ = Describe("GardenClientMap", func() {
	var (
		ctx        context.Context
		cm         clientmap.ClientMap
		key        clientmap.ClientSetKey
		factory    *internal.GardenClientSetFactory
		restConfig *rest.Config
	)

	BeforeEach(func() {
		ctx = context.TODO()
		key = keys.ForGarden()

		restConfig = &rest.Config{}
		factory = &internal.GardenClientSetFactory{
			RESTConfig: restConfig,
		}
		cm = internal.NewGardenClientMap(factory, logger.NewNopLogger())
	})

	Context("#GetClient", func() {
		It("should fail if ClientSetKey type is unsupported", func() {
			key = fakeKey{}
			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("unsupported ClientSetKey")))
		})

		It("should fail if NewClientSetWithConfig fails", func() {
			fakeErr := fmt.Errorf("fake")
			internal.NewClientSetWithConfig = func(fns ...kubernetes.ConfigFunc) (i kubernetes.Interface, err error) {
				return nil, fakeErr
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(err).To(MatchError(ContainSubstring("failed constructing new client for Garden cluster: fake")))
			Expect(cs).To(BeNil())
		})

		It("should correctly construct a new ClientSet", func() {
			fakeCS := fakeclientset.NewClientSetBuilder().WithRESTConfig(restConfig).Build()
			internal.NewClientSetWithConfig = func(fns ...kubernetes.ConfigFunc) (i kubernetes.Interface, err error) {
				return fakeCS, nil
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(cs).To(BeIdenticalTo(fakeCS))
			Expect(cs.RESTConfig()).To(Equal(restConfig))
		})
	})
})

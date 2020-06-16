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

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap/internal"
	"github.com/gardener/gardener/pkg/client/kubernetes/clientmap/keys"
	fakeclientset "github.com/gardener/gardener/pkg/client/kubernetes/fake"
	"github.com/gardener/gardener/pkg/logger"
	mockclient "github.com/gardener/gardener/pkg/mock/controller-runtime/client"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	baseconfig "k8s.io/component-base/config"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ShootClientMap", func() {
	var (
		ctx                 context.Context
		ctrl                *gomock.Controller
		mockGardenClient    *mockclient.MockClient
		mockSeedClient      *mockclient.MockClient
		fakeGardenClientSet *fakeclientset.ClientSet
		fakeSeedClientSet   *fakeclientset.ClientSet

		cm                     clientmap.ClientMap
		key                    clientmap.ClientSetKey
		factory                *internal.ShootClientSetFactory
		clientConnectionConfig baseconfig.ClientConnectionConfiguration

		shoot *gardencorev1beta1.Shoot
	)

	BeforeEach(func() {
		ctx = context.TODO()
		ctrl = gomock.NewController(GinkgoT())
		mockGardenClient = mockclient.NewMockClient(ctrl)
		mockSeedClient = mockclient.NewMockClient(ctrl)
		fakeGardenClientSet = fakeclientset.NewClientSetBuilder().WithClient(mockGardenClient).Build()
		fakeSeedClientSet = fakeclientset.NewClientSetBuilder().WithClient(mockSeedClient).Build()

		shoot = &gardencorev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "garden-of-eden",
				Name:      "forbidden-fruit",
			},
			Spec: gardencorev1beta1.ShootSpec{
				SeedName: pointer.StringPtr("apple-seed"),
			},
			Status: gardencorev1beta1.ShootStatus{
				TechnicalID: "shoot--garden-of-eden--forbidden-fruit",
			},
		}

		internal.ProjectForNamespaceWithClient = func(ctx context.Context, c client.Client, namespaceName string) (*gardencorev1beta1.Project, error) {
			return &gardencorev1beta1.Project{
				Spec: gardencorev1beta1.ProjectSpec{
					Namespace: pointer.StringPtr("garden-of-eden"),
				}}, nil
		}
		internal.LookupHost = func(host string) ([]string, error) {
			Expect(host).To(Equal("kube-apiserver." + shoot.Status.TechnicalID + ".svc"))
			return []string{"10.0.1.1"}, nil
		}

		key = keys.ForShoot(shoot)

		clientConnectionConfig = baseconfig.ClientConnectionConfiguration{
			Kubeconfig: "/var/run/secrets/kubeconfig",
		}
		factory = &internal.ShootClientSetFactory{
			GetGardenClient: func(ctx context.Context) (kubernetes.Interface, error) {
				return fakeGardenClientSet, nil
			},
			GetSeedClient: func(ctx context.Context, name string) (kubernetes.Interface, error) {
				Expect(name).To(Equal(*shoot.Spec.SeedName))
				return fakeSeedClientSet, nil
			},
			ClientConnectionConfig: clientConnectionConfig,
			Log:                    logger.NewNopLogger(),
		}
		cm = internal.NewShootClientMap(factory, logger.NewNopLogger())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("#GetClient", func() {
		It("should fail if ClientSetKey type is unsupported", func() {
			key = fakeKey{}
			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("unsupported ClientSetKey")))
		})

		It("should fail if GetGardenClient fails", func() {
			fakeErr := fmt.Errorf("fake")
			factory.GetGardenClient = func(ctx context.Context) (kubernetes.Interface, error) {
				return nil, fakeErr
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("failed to get garden client: fake")))
		})

		It("should fail if it cannot get Shoot object", func() {
			mockGardenClient.EXPECT().Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name}, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).
				Return(apierrors.NewNotFound(gardencorev1beta1.Resource("shoot"), shoot.Name))

			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("failed to get Shoot object")))
		})

		It("should fail if Shoot is not scheduled yet", func() {
			shoot.Spec.SeedName = nil
			mockGardenClient.EXPECT().Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name}, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).
				DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
					shoot.DeepCopyInto(obj.(*gardencorev1beta1.Shoot))
					return nil
				})

			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("shoot %q is not scheduled yet", key.Key()))))
		})

		It("should fail if GetSeedClient fails", func() {
			fakeErr := fmt.Errorf("fake")
			mockGardenClient.EXPECT().Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name}, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).
				DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
					shoot.DeepCopyInto(obj.(*gardencorev1beta1.Shoot))
					return nil
				})
			factory.GetSeedClient = func(ctx context.Context, name string) (kubernetes.Interface, error) {
				return nil, fakeErr
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("failed to get seed client: fake")))
		})

		It("should fail if ProjectForNamespaceWithClient fails", func() {
			fakeErr := fmt.Errorf("fake")
			mockGardenClient.EXPECT().Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name}, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).
				DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
					shoot.DeepCopyInto(obj.(*gardencorev1beta1.Shoot))
					return nil
				})
			internal.ProjectForNamespaceWithClient = func(ctx context.Context, c client.Client, namespaceName string) (*gardencorev1beta1.Project, error) {
				return nil, fakeErr
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring("failed to get Project for Shoot")))
		})

		It("should use external kubeconfig if LookupHost fails (out-of-cluster)", func() {
			fakeErr := fmt.Errorf("fake")
			mockGardenClient.EXPECT().Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name}, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).
				DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
					shoot.DeepCopyInto(obj.(*gardencorev1beta1.Shoot))
					return nil
				})
			internal.LookupHost = func(host string) ([]string, error) {
				return nil, fakeErr
			}

			internal.NewClientFromSecret = func(ctx context.Context, k8sClient kubernetes.Interface, namespace, secretName string, fns ...kubernetes.ConfigFunc) (kubernetes.Interface, error) {
				Expect(k8sClient).To(BeIdenticalTo(fakeSeedClientSet))
				Expect(namespace).To(Equal(shoot.Status.TechnicalID))
				Expect(secretName).To(Equal("gardener"))
				return nil, fakeErr
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("failed constructing new client for Shoot cluster %q: fake", key.Key()))))
		})

		It("should fall-back to external kubeconfig if internal kubeconfig is not found", func() {
			fakeErr := fmt.Errorf("fake")
			mockGardenClient.EXPECT().Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name}, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).
				DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
					shoot.DeepCopyInto(obj.(*gardencorev1beta1.Shoot))
					return nil
				})

			timesCalled := 0
			internal.NewClientFromSecret = func(ctx context.Context, k8sClient kubernetes.Interface, namespace, secretName string, fns ...kubernetes.ConfigFunc) (kubernetes.Interface, error) {
				if timesCalled == 0 {
					Expect(k8sClient).To(BeIdenticalTo(fakeSeedClientSet))
					Expect(namespace).To(Equal(shoot.Status.TechnicalID))
					Expect(secretName).To(Equal("gardener-internal"))
					timesCalled++
					return nil, apierrors.NewNotFound(corev1.Resource("secret"), "gardener-internal")
				} else {
					Expect(k8sClient).To(BeIdenticalTo(fakeSeedClientSet))
					Expect(namespace).To(Equal(shoot.Status.TechnicalID))
					Expect(secretName).To(Equal("gardener"))
					timesCalled++
					return nil, fakeErr
				}
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(timesCalled).To(Equal(2), "should call NewClientFromSecret twice (first with internal then with external kubeconfig)")
			Expect(cs).To(BeNil())
			Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("failed constructing new client for Shoot cluster %q: fake", key.Key()))))
		})

		It("should correctly construct a new ClientSet (in-cluster)", func() {
			fakeCS := fakeclientset.NewClientSet()
			mockGardenClient.EXPECT().Get(ctx, client.ObjectKey{Namespace: shoot.Namespace, Name: shoot.Name}, gomock.AssignableToTypeOf(&gardencorev1beta1.Shoot{})).
				DoAndReturn(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) error {
					shoot.DeepCopyInto(obj.(*gardencorev1beta1.Shoot))
					return nil
				})

			internal.NewClientFromSecret = func(ctx context.Context, k8sClient kubernetes.Interface, namespace, secretName string, fns ...kubernetes.ConfigFunc) (kubernetes.Interface, error) {
				Expect(k8sClient).To(BeIdenticalTo(fakeSeedClientSet))
				Expect(namespace).To(Equal(shoot.Status.TechnicalID))
				Expect(secretName).To(Equal("gardener-internal"))
				return fakeCS, nil
			}

			cs, err := cm.GetClient(ctx, key)
			Expect(err).NotTo(HaveOccurred())
			Expect(cs).To(BeIdenticalTo(fakeCS))
		})
	})
})

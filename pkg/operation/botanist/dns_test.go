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

package botanist_test

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	cr "github.com/gardener/gardener/pkg/chartrenderer"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	fakeclientset "github.com/gardener/gardener/pkg/client/kubernetes/fake"
	"github.com/gardener/gardener/pkg/operation"
	. "github.com/gardener/gardener/pkg/operation/botanist"
	"github.com/gardener/gardener/pkg/operation/garden"
	"github.com/gardener/gardener/pkg/operation/shoot"
	. "github.com/gardener/gardener/test/gomega"

	dnsv1alpha1 "github.com/gardener/external-dns-management/pkg/apis/dns/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("dns", func() {
	const (
		seedNS  = "test-ns"
		shootNS = "shoot-ns"
	)

	var (
		b                        *Botanist
		seedClient, gardenClient client.Client
		s                        *runtime.Scheme
		ctx                      context.Context
	)

	BeforeEach(func() {
		ctx = context.TODO()
		b = &Botanist{
			Operation: &operation.Operation{
				Shoot: &shoot.Shoot{
					Info: &v1beta1.Shoot{
						ObjectMeta: metav1.ObjectMeta{Namespace: shootNS},
					},
					SeedNamespace: seedNS,
				},
				Garden:         &garden.Garden{},
				Logger:         logrus.NewEntry(logrus.New()),
				ChartsRootPath: "../../../charts",
			},
		}

		s = runtime.NewScheme()
		Expect(dnsv1alpha1.AddToScheme(s)).NotTo(HaveOccurred())
		Expect(corev1.AddToScheme(s)).NotTo(HaveOccurred())

		gardenClient = fake.NewFakeClientWithScheme(scheme.Scheme)
		seedClient = fake.NewFakeClientWithScheme(s)

		renderer := cr.NewWithServerVersion(&version.Info{})
		chartApplier := kubernetes.NewChartApplier(renderer, kubernetes.NewApplier(seedClient, meta.NewDefaultRESTMapper([]schema.GroupVersion{})))
		Expect(chartApplier).NotTo(BeNil(), "should return chart applier")

		fakeClientSet := fakeclientset.NewClientSetBuilder().
			WithChartApplier(chartApplier).
			Build()

		b.K8sSeedClient = fakeClientSet
	})

	Context("DefaultExternalDNSProvider", func() {
		It("should create when calling Deploy and dns is enabled", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{Domain: pointer.StringPtr("foo")}
			b.Shoot.ExternalClusterDomain = pointer.StringPtr("baz")
			b.Shoot.ExternalDomain = &garden.Domain{Provider: "valid-provider"}

			Expect(b.DefaultExternalDNSProvider(seedClient).Deploy(ctx)).ToNot(HaveOccurred())

			found := &dnsv1alpha1.DNSProvider{}
			err := seedClient.Get(ctx, types.NamespacedName{Name: "external", Namespace: seedNS}, found)
			Expect(err).ToNot(HaveOccurred())

			expected := &dnsv1alpha1.DNSProvider{
				TypeMeta: metav1.TypeMeta{Kind: "DNSProvider", APIVersion: "dns.gardener.cloud/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "external",
					Namespace: seedNS,
					Annotations: map[string]string{
						// Should there be a comma after the last realm?
						"dns.gardener.cloud/realms": "test-ns,",
					},
					ResourceVersion: "1",
				},
				Spec: dnsv1alpha1.DNSProviderSpec{
					Type: "valid-provider",
					SecretRef: &corev1.SecretReference{
						Name: "extensions-dns-external",
					},
					Domains: &dnsv1alpha1.DNSSelection{
						Include: []string{"baz"},
					},
				},
			}
			Expect(found).To(DeepDerivativeEqual(expected))

		})
		It("should delete when calling Deploy and dns is disabled", func() {
			b.Shoot.DisableDNS = true
			Expect(seedClient.Create(ctx, &dnsv1alpha1.DNSProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: seedNS},
			})).NotTo(HaveOccurred())

			Expect(b.DefaultExternalDNSProvider(seedClient).Deploy(ctx)).ToNot(HaveOccurred())

			found := &dnsv1alpha1.DNSProvider{}
			err := seedClient.Get(ctx, types.NamespacedName{Name: "external", Namespace: seedNS}, found)
			Expect(err).To(BeNotFoundError())
		})
	})

	Context("DefaultInternalDNSProvider", func() {
		It("should create when calling Deploy and dns is enabled", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.DisableDNS = false
			b.Shoot.InternalClusterDomain = "foo.com"
			b.Garden.InternalDomain = &garden.Domain{
				Provider:     "valid-provider",
				IncludeZones: []string{"zone-a"},
				ExcludeZones: []string{"zone-b"},
			}

			Expect(b.DefaultInternalDNSProvider(seedClient).Deploy(ctx)).ToNot(HaveOccurred())

			found := &dnsv1alpha1.DNSProvider{}
			err := seedClient.Get(ctx, types.NamespacedName{Name: "internal", Namespace: seedNS}, found)
			Expect(err).ToNot(HaveOccurred())

			expected := &dnsv1alpha1.DNSProvider{
				TypeMeta: metav1.TypeMeta{Kind: "DNSProvider", APIVersion: "dns.gardener.cloud/v1alpha1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:            "internal",
					Namespace:       seedNS,
					ResourceVersion: "1",
				},
				Spec: dnsv1alpha1.DNSProviderSpec{
					Type: "valid-provider",
					SecretRef: &corev1.SecretReference{
						Name: "extensions-dns-internal",
					},
					Domains: &dnsv1alpha1.DNSSelection{
						Include: []string{"foo.com"},
					},
					Zones: &dnsv1alpha1.DNSSelection{
						Include: []string{"zone-a"},
						Exclude: []string{"zone-b"},
					},
				},
			}
			Expect(found).To(DeepDerivativeEqual(expected))

		})
		It("should delete when calling Deploy and dns is disabled", func() {
			b.Shoot.DisableDNS = true
			Expect(seedClient.Create(ctx, &dnsv1alpha1.DNSProvider{
				ObjectMeta: metav1.ObjectMeta{Name: "internal", Namespace: seedNS},
			})).ToNot(HaveOccurred())

			Expect(b.DefaultInternalDNSProvider(seedClient).Deploy(ctx)).ToNot(HaveOccurred())

			found := &dnsv1alpha1.DNSProvider{}
			err := seedClient.Get(ctx, types.NamespacedName{Name: "internal", Namespace: seedNS}, found)
			Expect(err).To(BeNotFoundError())
		})
	})

	Context("DefaultExternalDNSEntry", func() {
		It("should delete when calling Deploy", func() {
			Expect(seedClient.Create(ctx, &dnsv1alpha1.DNSEntry{
				ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: seedNS},
			})).ToNot(HaveOccurred())

			Expect(b.DefaultExternalDNSEntry(seedClient).Deploy(ctx)).ToNot(HaveOccurred())

			found := &dnsv1alpha1.DNSEntry{}
			err := seedClient.Get(ctx, types.NamespacedName{Name: "external", Namespace: seedNS}, found)
			Expect(err).To(BeNotFoundError())
		})
	})

	Context("DefaultInternalDNSEntry", func() {
		It("should delete when calling Deploy", func() {
			Expect(seedClient.Create(ctx, &dnsv1alpha1.DNSEntry{
				ObjectMeta: metav1.ObjectMeta{Name: "internal", Namespace: seedNS},
			})).ToNot(HaveOccurred())

			Expect(b.DefaultInternalDNSEntry(seedClient).Deploy(ctx)).ToNot(HaveOccurred())

			found := &dnsv1alpha1.DNSEntry{}
			err := seedClient.Get(ctx, types.NamespacedName{Name: "internal", Namespace: seedNS}, found)
			Expect(err).To(BeNotFoundError())
		})
	})

	Context("AdditionalDNSProviders", func() {
		It("should remove unneeded providers", func() {
			b.Shoot.DisableDNS = true

			providerOne := &dnsv1alpha1.DNSProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "to-remove",
					Namespace: seedNS,
					Labels: map[string]string{
						"gardener.cloud/role": "managed-dns-provider",
					},
				},
			}

			providerTwo := providerOne.DeepCopy()
			providerTwo.Name = "to-also-remove"

			Expect(seedClient.Create(ctx, providerOne)).ToNot(HaveOccurred())
			Expect(seedClient.Create(ctx, providerTwo)).ToNot(HaveOccurred())

			ap, err := b.AdditionalDNSProviders(ctx, nil, seedClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(ap).To(HaveLen(2))
			Expect(ap).To(HaveKey("to-remove"))
			Expect(ap).To(HaveKey("to-also-remove"))

			Expect(ap["to-remove"].Deploy(ctx)).NotTo(HaveOccurred(), "deploy (destroy) succeeds")
			Expect(ap["to-also-remove"].Deploy(ctx)).NotTo(HaveOccurred(), "deploy (destroy) succeeds")

			leftProviders := &dnsv1alpha1.DNSProviderList{}
			Expect(seedClient.List(ctx, leftProviders)).ToNot(HaveOccurred(), "listing of leftover providers succeeds")

			Expect(leftProviders.Items).To(BeEmpty())
		})

		It("should return error when provider is without Type", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{
				Providers: []v1beta1.DNSProvider{{}},
			}

			ap, err := b.AdditionalDNSProviders(ctx, gardenClient, seedClient)
			Expect(err).To(HaveOccurred())
			Expect(ap).To(HaveLen(0))
		})

		It("should return error when provider is without secretName", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{
				Providers: []v1beta1.DNSProvider{{
					Type: pointer.StringPtr("foo"),
				}},
			}

			ap, err := b.AdditionalDNSProviders(ctx, gardenClient, seedClient)
			Expect(err).To(HaveOccurred())
			Expect(ap).To(HaveLen(0))
		})

		It("should return error when provider is without secret", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{
				Providers: []v1beta1.DNSProvider{{
					Type:       pointer.StringPtr("foo"),
					SecretName: pointer.StringPtr("not-existing-secret"),
				}},
			}

			ap, err := b.AdditionalDNSProviders(ctx, gardenClient, seedClient)
			Expect(err).To(HaveOccurred())
			Expect(ap).To(HaveLen(0))
		})

		It("should add providers", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{
				Providers: []v1beta1.DNSProvider{
					{
						Type:    pointer.StringPtr("primary-skip"),
						Primary: pointer.BoolPtr(true),
					},
					{
						Type: pointer.StringPtr("unmanaged"),
					},
					{
						Type:       pointer.StringPtr("provider-one"),
						SecretName: pointer.StringPtr("secret-one"),
						Domains: &v1beta1.DNSIncludeExclude{
							Include: []string{"domain-1-include"},
							Exclude: []string{"domain-2-exclude"},
						},
						Zones: &v1beta1.DNSIncludeExclude{
							Include: []string{"zone-1-include"},
							Exclude: []string{"zone-1-exclude"},
						},
					},
					{
						Type:       pointer.StringPtr("provider-two"),
						SecretName: pointer.StringPtr("secret-two"),
					},
				},
			}

			Expect(seedClient.Create(ctx, &dnsv1alpha1.DNSProvider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "to-remove",
					Namespace: seedNS,
					Labels: map[string]string{
						"gardener.cloud/role": "managed-dns-provider",
					},
				},
			})).ToNot(HaveOccurred())

			secretOne := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-one",
					Namespace: shootNS,
				},
			}
			secretTwo := secretOne.DeepCopy()
			secretTwo.Name = "secret-two"

			Expect(gardenClient.Create(ctx, secretOne)).NotTo(HaveOccurred())
			Expect(gardenClient.Create(ctx, secretTwo)).NotTo(HaveOccurred())

			ap, err := b.AdditionalDNSProviders(ctx, gardenClient, seedClient)
			Expect(err).ToNot(HaveOccurred())
			Expect(ap).To(HaveLen(3))
			Expect(ap).To(HaveKey("to-remove"))
			Expect(ap).To(HaveKey("provider-one-secret-one"))
			Expect(ap).To(HaveKey("provider-two-secret-two"))

			for k, p := range ap {
				Expect(p.Deploy(ctx)).NotTo(HaveOccurred(), fmt.Sprintf("deploy of %s succeeds", k))
			}

			// can't use list - item[0]: can't assign or convert unstructured.Unstructured into v1alpha1.DNSProvider
			Expect(seedClient.Get(
				ctx,
				types.NamespacedName{Namespace: seedNS, Name: "to-remove"},
				&dnsv1alpha1.DNSProvider{},
			)).To(BeNotFoundError())

			providerOne := &dnsv1alpha1.DNSProvider{}
			Expect(seedClient.Get(
				ctx,
				types.NamespacedName{Namespace: seedNS, Name: "provider-one-secret-one"},
				providerOne,
			)).ToNot(HaveOccurred())

			Expect(providerOne.Spec.Domains).To(Equal(&dnsv1alpha1.DNSSelection{
				Include: []string{"domain-1-include"},
				Exclude: []string{"domain-2-exclude"},
			}))
			Expect(providerOne.Spec.Zones).To(Equal(&dnsv1alpha1.DNSSelection{
				Include: []string{"zone-1-include"},
				Exclude: []string{"zone-1-exclude"},
			}))

			Expect(seedClient.Get(
				ctx,
				types.NamespacedName{Namespace: seedNS, Name: "provider-two-secret-two"},
				&dnsv1alpha1.DNSProvider{},
			)).ToNot(HaveOccurred())
		})
	})

	Context("NeedsExternalDNS", func() {
		It("should be false when dns disabled", func() {
			b.Shoot.DisableDNS = true
			Expect(b.NeedsExternalDNS()).To(BeFalse())
		})

		It("should be false when Shoot's DNS is nil", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = nil
			Expect(b.NeedsExternalDNS()).To(BeFalse())
		})

		It("should be false when Shoot DNS's domain is nil", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{Domain: nil}
			Expect(b.NeedsExternalDNS()).To(BeFalse())
		})

		It("should be false when Shoot ExternalClusterDomain is nil", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{Domain: pointer.StringPtr("foo")}
			b.Shoot.ExternalClusterDomain = nil
			Expect(b.NeedsExternalDNS()).To(BeFalse())
		})

		It("should be false when Shoot ExternalClusterDomain is in nip.io", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{Domain: pointer.StringPtr("foo")}
			b.Shoot.ExternalClusterDomain = pointer.StringPtr("foo.nip.io")
			Expect(b.NeedsExternalDNS()).To(BeFalse())
		})

		It("should be false when Shoot ExternalDomain is nil", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{Domain: pointer.StringPtr("foo")}
			b.Shoot.ExternalClusterDomain = pointer.StringPtr("baz")
			b.Shoot.ExternalDomain = nil

			Expect(b.NeedsExternalDNS()).To(BeFalse())
		})

		It("should be false when Shoot ExternalDomain provider is unamanaged", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{Domain: pointer.StringPtr("foo")}
			b.Shoot.ExternalClusterDomain = pointer.StringPtr("baz")
			b.Shoot.ExternalDomain = &garden.Domain{Provider: "unmanaged"}

			Expect(b.NeedsExternalDNS()).To(BeFalse())
		})

		It("should be true when Shoot ExternalDomain provider is valid", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{Domain: pointer.StringPtr("foo")}
			b.Shoot.ExternalClusterDomain = pointer.StringPtr("baz")
			b.Shoot.ExternalDomain = &garden.Domain{Provider: "valid-provider"}

			Expect(b.NeedsExternalDNS()).To(BeTrue())
		})
	})

	Context("NeedsInternalDNS", func() {
		It("should be false when dns disabled", func() {
			b.Shoot.DisableDNS = true
			Expect(b.NeedsInternalDNS()).To(BeFalse())
		})

		It("should be false when the internal domain is nil", func() {
			b.Shoot.DisableDNS = false
			b.Garden.InternalDomain = nil
			Expect(b.NeedsInternalDNS()).To(BeFalse())
		})

		It("should be false when the internal domain provider is unmanaged", func() {
			b.Shoot.DisableDNS = false
			b.Garden.InternalDomain = &garden.Domain{Provider: "unmanaged"}
			Expect(b.NeedsInternalDNS()).To(BeFalse())
		})

		It("should be true when the internal domain provider is not unmanaged", func() {
			b.Shoot.DisableDNS = false
			b.Garden.InternalDomain = &garden.Domain{Provider: "some-provider"}
			Expect(b.NeedsInternalDNS()).To(BeTrue())
		})
	})

	Context("NeedsAdditionalDNSProviders", func() {
		It("should be false when dns disabled", func() {
			b.Shoot.DisableDNS = true
			Expect(b.NeedsAdditionalDNSProviders()).To(BeFalse())
		})

		It("should be false when Shoot's DNS is nil", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = nil
			Expect(b.NeedsAdditionalDNSProviders()).To(BeFalse())
		})

		It("should be false when there are no Shoot DNS Providers", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{}
			Expect(b.NeedsAdditionalDNSProviders()).To(BeFalse())
		})

		It("should be true when there are Shoot DNS Providers", func() {
			b.Shoot.DisableDNS = false
			b.Shoot.Info.Spec.DNS = &v1beta1.DNS{
				Providers: []v1beta1.DNSProvider{
					{Type: pointer.StringPtr("foo")},
					{Type: pointer.StringPtr("bar")},
				},
			}
			Expect(b.NeedsAdditionalDNSProviders()).To(BeTrue())
		})
	})
})

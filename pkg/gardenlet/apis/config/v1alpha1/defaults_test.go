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

package v1alpha1_test

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	componentbaseconfigv1alpha1 "k8s.io/component-base/config/v1alpha1"
	"k8s.io/klog"

	. "github.com/gardener/gardener/pkg/gardenlet/apis/config/v1alpha1"
)

var _ = Describe("Defaults", func() {
	Describe("GardenletConfiguration", func() {
		var obj *GardenletConfiguration

		BeforeEach(func() {
			obj = &GardenletConfiguration{}
		})

		It("should default the gardenlet configuration", func() {
			SetObjectDefaults_GardenletConfiguration(obj)

			Expect(obj.GardenClientConnection).NotTo(BeNil())
			Expect(obj.SeedClientConnection).NotTo(BeNil())
			Expect(obj.ShootClientConnection).NotTo(BeNil())
			Expect(obj.Controllers.BackupBucket).NotTo(BeNil())
			Expect(obj.Controllers.BackupEntry).NotTo(BeNil())
			Expect(obj.Controllers.ControllerInstallation).NotTo(BeNil())
			Expect(obj.Controllers.ControllerInstallationCare).NotTo(BeNil())
			Expect(obj.Controllers.ControllerInstallationRequired).NotTo(BeNil())
			Expect(obj.Controllers.Seed).NotTo(BeNil())
			Expect(obj.Controllers.Shoot).NotTo(BeNil())
			Expect(obj.Controllers.ShootCare).NotTo(BeNil())
			Expect(obj.Controllers.ShootStateSync).NotTo(BeNil())
			Expect(obj.Controllers.ManagedSeed).NotTo(BeNil())
			Expect(obj.LeaderElection).NotTo(BeNil())
			Expect(obj.LogLevel).To(PointTo(Equal("info")))
			Expect(obj.KubernetesLogLevel).To(PointTo(Equal(klog.Level(0))))
			Expect(obj.Server.HTTPS.BindAddress).To(Equal("0.0.0.0"))
			Expect(obj.Server.HTTPS.Port).To(Equal(2720))
			Expect(obj.SNI).ToNot(BeNil())
			Expect(obj.SNI.Ingress).ToNot(BeNil())
			Expect(obj.SNI.Ingress.Labels).To(Equal(map[string]string{"istio": "ingressgateway"}))
			Expect(obj.SNI.Ingress.Namespace).To(PointTo(Equal("istio-ingress")))
			Expect(obj.SNI.Ingress.ServiceName).To(PointTo(Equal("istio-ingressgateway")))
		})

		Describe("ClientConnection settings", func() {
			It("should not default ContentType and AcceptContentTypes", func() {
				SetObjectDefaults_GardenletConfiguration(obj)

				// ContentType fields will be defaulted by client constructors / controller-runtime based on whether a
				// given APIGroup supports protobuf or not. defaults must not touch these, otherwise the integelligent
				// logic will be overwritten
				Expect(obj.GardenClientConnection.ContentType).To(BeEmpty())
				Expect(obj.GardenClientConnection.AcceptContentTypes).To(BeEmpty())
				Expect(obj.SeedClientConnection.ContentType).To(BeEmpty())
				Expect(obj.SeedClientConnection.AcceptContentTypes).To(BeEmpty())
				Expect(obj.ShootClientConnection.ContentType).To(BeEmpty())
				Expect(obj.ShootClientConnection.AcceptContentTypes).To(BeEmpty())
			})
			It("should correctly default GardenClientConnection", func() {
				SetObjectDefaults_GardenletConfiguration(obj)
				Expect(obj.GardenClientConnection).To(Equal(&GardenClientConnection{
					ClientConnectionConfiguration: componentbaseconfigv1alpha1.ClientConnectionConfiguration{
						QPS:   50.0,
						Burst: 100,
					},
				}))
				Expect(obj.SeedClientConnection).To(Equal(&SeedClientConnection{
					ClientConnectionConfiguration: componentbaseconfigv1alpha1.ClientConnectionConfiguration{
						QPS:   50.0,
						Burst: 100,
					},
				}))
				Expect(obj.ShootClientConnection).To(Equal(&ShootClientConnection{
					ClientConnectionConfiguration: componentbaseconfigv1alpha1.ClientConnectionConfiguration{
						QPS:   50.0,
						Burst: 100,
					},
				}))
			})
		})
	})

	Describe("#SetDefaults_ManagedSeedControllerConfiguration", func() {
		var obj *ManagedSeedControllerConfiguration

		BeforeEach(func() {
			obj = &ManagedSeedControllerConfiguration{}
		})

		It("should default the configuration", func() {
			SetDefaults_ManagedSeedControllerConfiguration(obj)

			Expect(obj.ConcurrentSyncs).To(PointTo(Equal(DefaultControllerConcurrentSyncs)))
			Expect(obj.SyncJitterPeriod).To(PointTo(Equal(metav1.Duration{Duration: 5 * time.Minute})))
		})
	})

	Describe("#SetDefaults_ShootControllerConfiguration", func() {
		var obj *ShootControllerConfiguration

		BeforeEach(func() {
			obj = &ShootControllerConfiguration{}
		})

		It("should default the configuration", func() {
			SetDefaults_ShootControllerConfiguration(obj)

			Expect(obj.ConcurrentSyncs).To(PointTo(Equal(20)))
			Expect(obj.SyncPeriod).To(PointTo(Equal(metav1.Duration{Duration: time.Hour})))
			Expect(obj.RespectSyncPeriodOverwrite).To(PointTo(Equal(false)))
			Expect(obj.ReconcileInMaintenanceOnly).To(PointTo(Equal(false)))
			Expect(obj.RetryDuration).To(PointTo(Equal(metav1.Duration{Duration: 12 * time.Hour})))
			Expect(obj.DNSEntryTTLSeconds).To(PointTo(Equal(int64(120))))
		})
	})
})

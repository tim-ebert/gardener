// Copyright (c) 2020 SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package webhooks_test

import (
	"io"
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"go.uber.org/zap/zapcore"
	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apiserver/pkg/authentication/serviceaccount"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	apisconfig "github.com/gardener/gardener/pkg/admissioncontroller/apis/config"
	. "github.com/gardener/gardener/pkg/admissioncontroller/webhooks"
	gardencorev1alpha1 "github.com/gardener/gardener/pkg/apis/core/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
)

var _ = Describe("ValidateResourceSizeHandler", func() {
	var (
		request admission.Request
		decoder *admission.Decoder

		validator *ObjectSizeHandler

		logBuffer   *gbytes.Buffer
		testEncoder runtime.Encoder

		projectsSizeLimit, _ = resource.ParseQuantity("0M")
		secretSizeLimit, _   = resource.ParseQuantity("1Mi")
		// size of shoot w/ namespace, name, w/o spec
		shootsv1beta1SizeLimit, _ = resource.ParseQuantity("405")
		// size of shoot w/ namespace, name, w/o spec -1 byte
		shootsv1alpha1SizeLimit, _ = resource.ParseQuantity("405")

		restrictedUserName                  = "restrictedUser"
		unrestrictedUserName                = "unrestrictedUser"
		restrictedGroupName                 = "restrictedGroup"
		unrestrictedGroupName               = "unrestrictedGroup"
		restrictedServiceAccountName        = "restrictedServiceAccount"
		unrestrictedServiceAccountName      = "unrestrictedServiceAccount"
		unrestrictedServiceAccountNamespace = "unrestricted"

		config = func() *apisconfig.ResourceAdmissionConfiguration {
			return &apisconfig.ResourceAdmissionConfiguration{
				UnrestrictedSubjects: []rbacv1.Subject{
					{
						Kind: rbacv1.GroupKind,
						Name: unrestrictedGroupName,
					},
					{
						Kind: rbacv1.UserKind,
						Name: unrestrictedUserName,
					},
					{
						Kind:      rbacv1.ServiceAccountKind,
						Name:      unrestrictedServiceAccountName,
						Namespace: unrestrictedServiceAccountNamespace,
					},
				},
				Limits: []apisconfig.ResourceLimit{
					{
						APIGroups:   []string{"*"},
						APIVersions: []string{"*"},
						Resources:   []string{"projects"},
						Size:        projectsSizeLimit,
					},
					{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"secrets"},
						Size:        secretSizeLimit,
					},
					{
						APIGroups:   []string{"core.gardener.cloud"},
						APIVersions: []string{"v1beta1"},
						Resources:   []string{"shoots", "plants"},
						Size:        shootsv1beta1SizeLimit,
					},
					{
						APIGroups:   []string{"core.gardener.cloud"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"shoots"},
						Size:        shootsv1alpha1SizeLimit,
					},
				},
			}
		}

		empty = func() runtime.Object {
			return nil
		}

		shootv1beta1 = func() runtime.Object {
			return &gardencorev1beta1.Shoot{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Shoot",
					APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "garden-my-project",
					Name:      "my-shoot",
				},
			}
		}

		shootv1alpha1 = func() runtime.Object {
			return &gardencorev1alpha1.Shoot{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Shoot",
					APIVersion: gardencorev1alpha1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "garden-my-project",
					Name:      "my-shoot",
				},
			}
		}

		project = func() runtime.Object {
			return &gardencorev1beta1.Project{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Project",
					APIVersion: gardencorev1beta1.SchemeGroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-project",
				},
			}
		}

		secret = func() runtime.Object {
			return &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: corev1.SchemeGroupVersion.String(),
				},
			}
		}

		configMap = func() runtime.Object {
			return &corev1.ConfigMap{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ConfigMap",
					APIVersion: corev1.SchemeGroupVersion.String(),
				},
			}
		}

		unrestrictedUser = func() authenticationv1.UserInfo {
			return authenticationv1.UserInfo{
				Username: unrestrictedUserName,
				Groups:   []string{"test"},
			}
		}

		unrestrictedGroup = func() authenticationv1.UserInfo {
			return authenticationv1.UserInfo{
				Username: "restricted",
				Groups:   []string{unrestrictedGroupName},
			}
		}

		unrestrictedServiceAccount = func() authenticationv1.UserInfo {
			return authenticationv1.UserInfo{
				Username: serviceaccount.MakeUsername(unrestrictedServiceAccountNamespace, unrestrictedServiceAccountName),
				Groups:   serviceaccount.MakeGroupNames(unrestrictedGroupName),
			}
		}

		restrictedServiceAccount = func() authenticationv1.UserInfo {
			return authenticationv1.UserInfo{
				Username: serviceaccount.MakeUsername(unrestrictedServiceAccountNamespace, restrictedServiceAccountName),
				Groups:   serviceaccount.MakeGroupNames(restrictedGroupName),
			}
		}

		restrictedUser = func() authenticationv1.UserInfo {
			return authenticationv1.UserInfo{
				Username: restrictedUserName,
				Groups:   []string{restrictedGroupName},
			}
		}
	)

	BeforeEach(func() {
		var err error
		decoder, err = admission.NewDecoder(kubernetes.GardenScheme)
		Expect(err).NotTo(HaveOccurred())

		validator = &ObjectSizeHandler{Config: config()}

		logBuffer = gbytes.NewBuffer()
		logger := logzap.New(logzap.UseDevMode(true), logzap.WriteTo(io.MultiWriter(GinkgoWriter, logBuffer)), logzap.Level(zapcore.Level(0)))
		Expect(inject.LoggerInto(logger, validator)).To(BeTrue())
		Expect(admission.InjectDecoderInto(decoder, validator)).To(BeTrue())

		testEncoder = &json.Serializer{}
		request = admission.Request{}
		request.Operation = admissionv1.Update
	})

	test := func(objFn func() runtime.Object, subresource string, userFn func() authenticationv1.UserInfo, expectedAllowed bool, expectedMsg string) {
		if obj := objFn(); obj != nil {
			objData, err := runtime.Encode(testEncoder, obj)
			Expect(err).NotTo(HaveOccurred())
			request.Object.Raw = objData

			gvr, _ := meta.UnsafeGuessKindToResource(obj.GetObjectKind().GroupVersionKind())
			v1Gvr := metav1.GroupVersionResource{
				Group:    gvr.Group,
				Version:  gvr.Version,
				Resource: gvr.Resource,
			}

			request.Resource = v1Gvr
			request.RequestResource = &v1Gvr
			request.Object = runtime.RawExtension{Raw: objData}

			if o, ok := obj.(client.Object); ok {
				request.Name = o.GetName()
				request.Namespace = o.GetNamespace()
			}
		}

		request.SubResource = subresource
		request.UserInfo = userFn()
		response := validator.Handle(ctx, request)
		Expect(response).To(Not(BeNil()))
		Expect(response.Allowed).To(Equal(expectedAllowed))
		var expectedStatusCode int32 = http.StatusOK
		if !expectedAllowed {
			expectedStatusCode = http.StatusUnprocessableEntity
		}
		Expect(response.Result.Code).To(Equal(expectedStatusCode))
		if expectedMsg != "" {
			Expect(response.Result.Message).To(ContainSubstring(expectedMsg))
		}
	}

	Context("ignored requests", func() {
		It("should ignore subresources", func() {
			test(project, "logs", restrictedUser, true, "subresource")
		})
		It("empty resource", func() {
			test(empty, noSubResource, restrictedUser, true, "no limit")
		})
	})

	It("should pass because size is in range for v1beta1 shoot", func() {
		test(shootv1beta1, noSubResource, restrictedUser, true, "resource size ok")
	})
	It("should fail because size is not in range for v1alpha1 shoot and mode is nil", func() {
		test(shootv1alpha1, noSubResource, restrictedUser, false, "resource size exceeded")
		Eventually(logBuffer).Should(gbytes.Say("maximum resource size exceeded"))
	})
	It("should fail because size is not in range for v1alpha1 shoot and mode is block", func() {
		blockMode := apisconfig.ResourceAdmissionWebhookMode("block")
		validator.Config.OperationMode = &blockMode
		test(shootv1alpha1, noSubResource, restrictedUser, false, "resource size exceeded")
		Eventually(logBuffer).Should(gbytes.Say("maximum resource size exceeded"))
	})
	It("should pass but log because size is not in range for v1alpha1 shoot and mode is log", func() {
		logMode := apisconfig.ResourceAdmissionWebhookMode("log")
		validator.Config.OperationMode = &logMode
		test(shootv1alpha1, noSubResource, restrictedUser, true, "resource size ok")
		Eventually(logBuffer).Should(gbytes.Say("maximum resource size exceeded"))
	})
	It("should pass because request is for status subresource of v1alpha1 shoot", func() {
		test(shootv1alpha1, "status", restrictedUser, true, "subresource")
	})
	It("should pass because size is in range for secret", func() {
		test(secret, noSubResource, restrictedUser, true, "resource size ok")
	})
	It("should pass because no limits configured for configMaps", func() {
		test(configMap, noSubResource, restrictedUser, true, "no limit")
	})
	It("should fail because size is not in range for project", func() {
		test(project, noSubResource, restrictedUser, false, "resource size exceeded")
	})
	It("should pass because of unrestricted user", func() {
		test(project, noSubResource, unrestrictedUser, true, "unrestricted")
	})
	It("should pass because of unrestricted group", func() {
		test(project, noSubResource, unrestrictedGroup, true, "unrestricted")
	})
	It("should pass because of unrestricted service account", func() {
		test(project, noSubResource, unrestrictedServiceAccount, true, "unrestricted")
	})
	It("should fail because of restricted service account", func() {
		test(project, noSubResource, restrictedServiceAccount, false, "resource size exceeded")
	})
})

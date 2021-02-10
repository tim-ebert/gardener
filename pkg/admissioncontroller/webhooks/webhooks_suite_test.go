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
	"context"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/gardener/gardener/cmd/utils"
	"github.com/gardener/gardener/test/framework"
)

func TestWebhooks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gardener Admission Controller Webhooks Suite")
}

var (
	ctx        context.Context
	ctxCancel  context.CancelFunc
	err        error
	logger     logr.Logger
	testEnv    *envtest.Environment
	restConfig *rest.Config
)

var _ = BeforeSuite(func() {
	utils.DeduplicateWarnings()
	ctx, ctxCancel = context.WithCancel(context.Background())

	logger = logzap.New(logzap.UseDevMode(true), logzap.WriteTo(GinkgoWriter), logzap.Level(zapcore.Level(0)))
	// enable manager and envtest logs
	logf.SetLogger(logger)

	By("starting test environment")
	testEnv = &envtest.Environment{
		// WebhookInstallOptions: envtest.WebhookInstallOptions{
		// 	ValidatingWebhooks: []client.Object{getValidatingWebhookConfig()},
		// 	MutatingWebhooks:   []client.Object{getMutatingWebhookConfig()},
		// },
	}
	// restConfig, err = testEnv.Start()
	// Expect(err).ToNot(HaveOccurred())
	// Expect(restConfig).ToNot(BeNil())

	By("setting up manager")
	// setup manager in order to leverage dependency injection
	// mgr, err := manager.New(restConfig, manager.Options{
	// 	Port:    testEnv.WebhookInstallOptions.LocalServingPort,
	// 	Host:    testEnv.WebhookInstallOptions.LocalServingHost,
	// 	CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
	// })
	// Expect(err).NotTo(HaveOccurred())

	By("setting up webhook server")
	// server := mgr.GetWebhookServer()
	// server.Register(seedadmission.ExtensionDeletionProtectionWebhookPath, &webhook.Admission{Handler: &seedadmission.ExtensionDeletionProtection{}})
	// server.Register(seedadmission.GardenerShootControlPlaneSchedulerWebhookPath, &webhook.Admission{Handler: admission.HandlerFunc(seedadmission.DefaultShootControlPlanePodsSchedulerName)})

	// go func() {
	// 	Expect(server.Start(ctx)).To(Succeed())
	// }()
})

var _ = AfterSuite(func() {
	By("running cleanup actions")
	framework.RunCleanupActions()

	By("stopping manager")
	ctxCancel()

	By("stopping test environment")
	Expect(testEnv.Stop()).To(Succeed())
})

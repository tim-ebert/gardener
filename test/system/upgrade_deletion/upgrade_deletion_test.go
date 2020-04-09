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

/**
	Overview
		- Tests the deletion of a Shoot after a Gardener upgrade

	Steps
		- Creates a Shoot and waits for it to be completely reconciled
		- Waits for the Seed's Gardenlet version to be updated
		- Deletes the Shoot and waits for it to be completely deleted

	Test: Shoot deletion after Gardener Upgrade
	Expected Output
		- Successful Shoot deletion
 **/

package upgrade_deletion

import (
	"context"
	"time"

	"github.com/gardener/gardener/test/framework"

	. "github.com/onsi/ginkgo"
)

const (
	testTimeout               = 4 * time.Hour
	createAndReconcileTimeout = 1 * time.Hour
	seedUpdateTimeout         = 1 * time.Hour
)

func init() {
	framework.RegisterShootCreationFrameworkFlags()
}

var _ = Describe("Shoot Creation testing", func() {

	f := framework.NewShootCreationFramework(&framework.ShootCreationConfig{
		GardenerConfig: &framework.GardenerConfig{
			CommonConfig: &framework.CommonConfig{
				ResourceDir: "../../framework/resources",
			},
		},
	})

	framework.CIt("Create and Reconcile Shoot", func(ctx context.Context) {
		By("Creating shoot and waiting for it to be reconciled")
		framework.WithTimeout(func(ctx context.Context) {
			_, err := f.CreateShoot(ctx)
			framework.ExpectNoError(err)
		}, createAndReconcileTimeout)

		shootFramework := f.GetShootFramework()

		By("Waiting for Seed's Gardenlet version to be updated")
		err := f.WaitForSeedGardenletVersionToBeUpdated(ctx, shootFramework.Seed, seedUpdateTimeout)
		framework.ExpectNoError(err)

		By("Deleting Shoot and waiting for it to be deleted")
		err = f.DeleteShootAndWaitForDeletion(ctx, shootFramework.Shoot)
		framework.ExpectNoError(err)
	}, testTimeout)
})
